# Configuracion y funciones comunes de los scripts de encendido/apagado en GCP.
# Se carga con: . "$PSScriptRoot\config.ps1"
# Requiere gcloud instalado y con sesion iniciada (gcloud auth login).

$Project     = 'proyecto-plataforma-mooc'
$SqlInstance = 'mooc-db-instance'
$ApiMig      = 'mooc-api-mig'
$WorkerMig   = 'mooc-worker-mig'

# Vacio = se detecta solo. Redis: la unica VM suelta cuyo nombre contenga "redis".
# Zona: la del MIG de la API.
$RedisVm = ''
$Zone    = ''

# Autoescalado: cada MIG con su propia senal (Set-ApiAutoscaling / Set-WorkerAutoscaling)
$MinReplicas = 1
$MaxReplicas = 3

# API: CPU media del grupo
$TargetCpu   = 0.6
$CoolDown    = 120

# Workers: trabajos en la cola Asynq (pendientes + activos) que publica el
# exportador de la VM de Redis (backend/cmd/queue-metrics). El autoscaler pide
# ceil(backlog / WorkerJobsPerVm) VMs: 0-2 -> 1, 3-4 -> 2, 5 o mas -> 3.
$WorkerBacklogMetric = 'custom.googleapis.com/mooc/asynq/backlog'
$WorkerJobsPerVm     = 2      # = WORKER_CONCURRENCY de cada worker (e2-highcpu-2)
$WorkerCoolDown      = 180

# Reduccion escalonada en ambos grupos: como maximo 1 VM menos cada 5 minutos
$ScaleInMaxReplicas = 1
$ScaleInWindowSec   = 300

function Write-Step([string]$Text) { Write-Host ""; Write-Host "==> $Text" -ForegroundColor Cyan }
function Write-Ok([string]$Text)   { Write-Host "    $Text" -ForegroundColor Green }
function Write-Warn([string]$Text) { Write-Host "    $Text" -ForegroundColor Yellow }

# Ejecuta gcloud; si falla y no se pidio -AllowFail, detiene el script.
function Invoke-G {
    param([string[]]$GArgs, [switch]$AllowFail)
    & gcloud @GArgs
    if ($LASTEXITCODE -ne 0 -and -not $AllowFail) {
        throw "gcloud fallo (codigo $LASTEXITCODE): gcloud $($GArgs -join ' ')"
    }
}

# Ejecuta gcloud y devuelve su salida como texto (sin errores en pantalla).
function Get-G {
    param([string[]]$GArgs)
    $out = & gcloud @GArgs 2>$null
    if ($LASTEXITCODE -ne 0) { return '' }
    return (($out | Out-String).Trim())
}

function Initialize-Gcp {
    if (-not (Get-Command gcloud -ErrorAction SilentlyContinue)) {
        throw 'No se encontro gcloud. Instala Google Cloud CLI y ejecuta: gcloud auth login'
    }
    $account = Get-G @('config', 'get-value', 'account')
    if (-not $account) { throw 'No hay sesion de gcloud. Ejecuta: gcloud auth login' }
    Write-Host "Proyecto: $Project   Cuenta: $account"

    if (-not $script:Zone) {
        $script:Zone = Get-G @('compute', 'instance-groups', 'managed', 'list', "--project=$Project",
            "--filter=name=$ApiMig", '--format=value(zone.basename())')
        if (-not $script:Zone) { throw "No se encontro el MIG '$ApiMig' en el proyecto '$Project'. Revisa config.ps1." }
    }
    Write-Host "Zona: $script:Zone"

    if (-not $script:RedisVm) {
        $rows = Get-G @('compute', 'instances', 'list', "--project=$Project",
            '--filter=name~redis', '--format=value(name)')
        $names = @($rows -split "`r?`n" | Where-Object { $_ })
        if ($names.Count -ne 1) {
            throw "No pude detectar la VM de Redis (encontre: $($names -join ', ')). Escribe su nombre en `$RedisVm en config.ps1."
        }
        $script:RedisVm = $names[0]
    }
    Write-Host "Redis: $script:RedisVm   Cloud SQL: $SqlInstance"
}

function Get-SqlPolicy { Get-G @('sql', 'instances', 'describe', $SqlInstance, "--project=$Project", '--format=value(settings.activationPolicy)') }
function Get-SqlState  { Get-G @('sql', 'instances', 'describe', $SqlInstance, "--project=$Project", '--format=value(state)') }
function Get-SqlPendingOps {
    Get-G @('sql', 'operations', 'list', "--instance=$SqlInstance", "--project=$Project",
        '--filter=status!=DONE', '--format=value(name)')
}

function Get-LbIp {
    $ip = Get-G @('compute', 'forwarding-rules', 'list', '--global', "--project=$Project", '--format=value(IPAddress)')
    return (($ip -split "`r?`n" | Where-Object { $_ } | Select-Object -First 1))
}

# --- Politicas de autoescalado ------------------------------------------------

function Set-ApiAutoscaling {
    Invoke-G @('compute', 'instance-groups', 'managed', 'set-autoscaling', $ApiMig, "--project=$Project", "--zone=$script:Zone",
        "--min-num-replicas=$MinReplicas", "--max-num-replicas=$MaxReplicas",
        "--target-cpu-utilization=$TargetCpu", "--cool-down-period=$CoolDown",
        "--scale-in-control=max-scaled-in-replicas=$ScaleInMaxReplicas,time-window=$ScaleInWindowSec", '--mode=on')
}

# Metrica por grupo (recurso "global"): describe la cola entera, no una VM.
function Set-WorkerAutoscaling {
    Invoke-G @('compute', 'instance-groups', 'managed', 'set-autoscaling', $WorkerMig, "--project=$Project", "--zone=$script:Zone",
        "--min-num-replicas=$MinReplicas", "--max-num-replicas=$MaxReplicas",
        "--cool-down-period=$WorkerCoolDown",
        "--update-stackdriver-metric=$WorkerBacklogMetric",
        '--stackdriver-metric-filter=resource.type = global',
        "--stackdriver-metric-single-instance-assignment=$WorkerJobsPerVm",
        "--scale-in-control=max-scaled-in-replicas=$ScaleInMaxReplicas,time-window=$ScaleInWindowSec", '--mode=on')
}

# --- Acceso a la API REST de Compute (para lo que gcloud no expone) -----------

function Get-AccessToken {
    if (-not $script:AccessToken) {
        $script:AccessToken = Get-G @('auth', 'print-access-token')
        if (-not $script:AccessToken) { throw 'No pude obtener un token de gcloud. Ejecuta: gcloud auth login' }
    }
    return $script:AccessToken
}

function Get-GcpJson([string]$Url) {
    return Invoke-RestMethod -Uri $Url -Headers @{ Authorization = "Bearer $(Get-AccessToken)" }
}

# URL completa de la plantilla de instancia que usa un MIG.
function Get-MigTemplateUrl([string]$Mig) {
    $url = Get-G @('compute', 'instance-groups', 'managed', 'describe', $Mig, "--project=$Project", "--zone=$script:Zone",
        '--format=value(instanceTemplate)')
    if (-not $url) { throw "No pude leer la plantilla del MIG $Mig." }
    return $url
}

function Get-MetadataValue($Properties, [string]$Key) {
    $item = @($Properties.metadata.items) | Where-Object { $_.key -eq $Key } | Select-Object -First 1
    if ($item) { return $item.value }
    return $null
}

# Autoscaler del MIG (politica efectiva, tamano recomendado y avisos), o $null si no tiene.
function Get-MigAutoscaler([string]$Mig) {
    $url = Get-G @('compute', 'instance-groups', 'managed', 'describe', $Mig, "--project=$Project", "--zone=$script:Zone",
        '--format=value(status.autoscaler)')
    if (-not $url) { return $null }
    return Get-GcpJson $url
}

function Format-AutoscalingPolicy($Autoscaler) {
    if (-not $Autoscaler) { return 'sin autoescalado' }
    $p = $Autoscaler.autoscalingPolicy
    $senales = @()
    if ($p.cpuUtilization -and $p.cpuUtilization.utilizationTarget) { $senales += "CPU $($p.cpuUtilization.utilizationTarget)" }
    if ($p.loadBalancingUtilization) { $senales += "balanceador $($p.loadBalancingUtilization.utilizationTarget)" }
    foreach ($m in @($p.customMetricUtilizations)) {
        if (-not $m) { continue }
        if ($m.singleInstanceAssignment) { $senales += "$($m.metric) ($($m.singleInstanceAssignment) por VM)" }
        else { $senales += "$($m.metric) (objetivo $($m.utilizationTarget))" }
    }
    $reduccion = ''
    if ($p.scaleInControl -and $p.scaleInControl.maxScaledInReplicas) {
        $reduccion = " reduccion<=$($p.scaleInControl.maxScaledInReplicas.fixed)/$($p.scaleInControl.timeWindowSec)s"
    }
    return ("modo={0} min={1} max={2} inicializacion={3}s{4} senal: {5}" -f
        $p.mode, $p.minNumReplicas, $p.maxNumReplicas, $p.coolDownPeriodSec, $reduccion, ($senales -join ' + '))
}

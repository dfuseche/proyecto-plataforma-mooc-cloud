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

# Autoescalado de los dos MIGs
$MinReplicas = 1
$MaxReplicas = 3
$TargetCpu   = 0.6
$CoolDown    = 120

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

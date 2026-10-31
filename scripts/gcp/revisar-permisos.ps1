# Revisa que las cuentas de servicio tengan lo que necesitan los cambios del
# Escenario 3 y muestra (o aplica con -Aplicar) los comandos que faltan:
#   - Plantillas de los MIG: roles/logging.logWriter, para que los contenedores
#     envien sus logs a Cloud Logging (driver gcplogs en startup.sh).
#   - VM de Redis: roles/monitoring.metricWriter (exportador de la cola),
#     roles/logging.logWriter y roles/artifactregistry.reader (bajar la imagen).
# Ademas revisa los "scopes" de cada VM: sin el scope, el rol no alcanza.
# Uso:  .\scripts\gcp\revisar-permisos.ps1 [-Aplicar]
#   -Aplicar  asigna los roles que falten (requiere ser owner o IAM admin del proyecto)

param([switch]$Aplicar)

. "$PSScriptRoot\config.ps1"
Initialize-Gcp

$scopeTodo = 'https://www.googleapis.com/auth/cloud-platform'
$faltantes = 0

function Get-SaRoles([string]$Sa) {
    $out = Get-G @('projects', 'get-iam-policy', $Project, '--flatten=bindings[].members',
        "--filter=bindings.members:serviceAccount:$Sa", '--format=value(bindings.role)')
    return @($out -split "`r?`n" | Where-Object { $_ })
}

# $ScopesNecesarios: cada elemento es una lista de scopes alternativos (basta uno);
# cloud-platform los cubre todos.
function Test-Cuenta([string]$Quien, $Sa, [string[]]$Roles, [object[]]$ScopesNecesarios) {
    Write-Step "$Quien -> $($Sa.email)"
    $actuales = Get-SaRoles $Sa.email
    $amplio = @($actuales | Where-Object { $_ -in @('roles/owner', 'roles/editor') }).Count -gt 0
    foreach ($r in $Roles) {
        if ($actuales -contains $r -or $amplio) {
            Write-Ok "OK     $r"
            continue
        }
        $script:faltantes++
        Write-Warn "FALTA  $r"
        $cmd = @('projects', 'add-iam-policy-binding', $Project, "--member=serviceAccount:$($Sa.email)", "--role=$r", '--condition=None', '--quiet')
        if ($Aplicar) {
            Invoke-G $cmd | Out-Null
            Write-Ok "asignado $r"
        } else {
            Write-Host "      gcloud $($cmd -join ' ')"
        }
    }

    $scopes = @($Sa.scopes)
    if ($scopes -contains $scopeTodo) {
        Write-Ok 'OK     scope cloud-platform'
        return
    }
    foreach ($alternativas in $ScopesNecesarios) {
        $tiene = @($alternativas | Where-Object { $scopes -contains $_ }).Count -gt 0
        if ($tiene) { Write-Ok "OK     scope $($alternativas[0])" }
        else {
            $script:faltantes++
            Write-Warn "FALTA  scope $($alternativas -join ' o ')"
        }
    }
}

$logScope = @('https://www.googleapis.com/auth/logging.write', 'https://www.googleapis.com/auth/logging.admin')

foreach ($mig in @($ApiMig, $WorkerMig)) {
    $tpl = Get-GcpJson (Get-MigTemplateUrl $mig)
    $sa = @($tpl.properties.serviceAccounts)[0]
    if (-not $sa) { Write-Warn "La plantilla de $mig no tiene cuenta de servicio."; $faltantes++; continue }
    Test-Cuenta "Plantilla de $mig ($($tpl.name))" $sa @('roles/logging.logWriter') @(, $logScope)
}

$redis = Get-G @('compute', 'instances', 'describe', $script:RedisVm, "--project=$Project", "--zone=$script:Zone", '--format=json') | ConvertFrom-Json
$redisSa = @($redis.serviceAccounts)[0]
if (-not $redisSa) {
    Write-Warn "La VM $script:RedisVm no tiene cuenta de servicio."
    $faltantes++
} else {
    Test-Cuenta "VM de Redis ($script:RedisVm)" $redisSa `
        @('roles/monitoring.metricWriter', 'roles/logging.logWriter', 'roles/artifactregistry.reader') `
        @(
            @('https://www.googleapis.com/auth/monitoring.write', 'https://www.googleapis.com/auth/monitoring'),
            $logScope,
            @('https://www.googleapis.com/auth/devstorage.read_only', 'https://www.googleapis.com/auth/devstorage.read_write', 'https://www.googleapis.com/auth/devstorage.full_control')
        )
}

Write-Host ""
if ($faltantes -eq 0) {
    Write-Host 'Todo en orden.' -ForegroundColor Green
} else {
    Write-Host "Hay $faltantes punto(s) pendiente(s)." -ForegroundColor Yellow
    Write-Host '  - Un rol que ya este asignado a nivel del repositorio de Artifact Registry puede aparecer como FALTA: en ese caso ignoralo.'
    Write-Host '  - Un scope faltante en la VM de Redis se corrige con la VM detenida (ojo: detiene la cola):'
    Write-Host "      gcloud compute instances stop $script:RedisVm --zone=$script:Zone --project=$Project"
    Write-Host "      gcloud compute instances set-service-account $script:RedisVm --zone=$script:Zone --project=$Project --service-account=<cuenta> --scopes=cloud-platform"
    Write-Host "      gcloud compute instances start $script:RedisVm --zone=$script:Zone --project=$Project"
    Write-Host '  - Un scope faltante en una plantilla requiere crear la plantilla con otro scope (avisar a quien la creo).'
}

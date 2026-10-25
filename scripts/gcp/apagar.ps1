# Apaga el proyecto en GCP para no gastar: MIGs a 0 (sin autoescalado), VM de Redis y Cloud SQL.
# Los datos se conservan (disco de Redis y almacenamiento de Cloud SQL siguen cobrando poco).
# Uso:  .\scripts\gcp\apagar.ps1        (pide confirmacion)   |   .\scripts\gcp\apagar.ps1 -Yes

param([switch]$Yes)

. "$PSScriptRoot\config.ps1"
Initialize-Gcp

if (-not $Yes) {
    $r = Read-Host 'Se apagaran MIGs, Redis y Cloud SQL. Escribe SI para continuar'
    if ($r -ne 'SI') { Write-Host 'Cancelado.'; return }
}

Write-Step '1/4 Quitando el autoescalado y llevando los MIGs a 0'
foreach ($mig in @($ApiMig, $WorkerMig)) {
    Invoke-G @('compute', 'instance-groups', 'managed', 'stop-autoscaling', $mig, "--project=$Project", "--zone=$script:Zone") -AllowFail
    Invoke-G @('compute', 'instance-groups', 'managed', 'resize', $mig, '--size=0', "--project=$Project", "--zone=$script:Zone")
    Write-Ok "$mig en 0."
}

Write-Step '2/4 Esperando a que los MIGs queden en 0'
foreach ($mig in @($ApiMig, $WorkerMig)) {
    Invoke-G @('compute', 'instance-groups', 'managed', 'wait-until', $mig, '--stable', "--project=$Project", "--zone=$script:Zone", '--timeout=600')
}

Write-Step "3/4 Deteniendo la VM de Redis ($script:RedisVm)"
Invoke-G @('compute', 'instances', 'stop', $script:RedisVm, "--project=$Project", "--zone=$script:Zone", '--quiet')

Write-Step "4/4 Apagando Cloud SQL ($SqlInstance)"
Invoke-G @('sql', 'instances', 'patch', $SqlInstance, "--project=$Project", '--activation-policy=NEVER', '--quiet')

Write-Step 'Verificacion: VMs que siguen encendidas en el proyecto'
$corriendo = Get-G @('compute', 'instances', 'list', "--project=$Project", '--filter=status=RUNNING', '--format=table(name,zone.basename(),machineType.basename())')
if ($corriendo) { Write-Warn 'Estas VMs siguen encendidas (por ejemplo la del generador de carga):'; Write-Host $corriendo }
else { Write-Ok 'No queda ninguna VM encendida.' }

Write-Host ""
Write-Host 'Proyecto apagado.' -ForegroundColor Green

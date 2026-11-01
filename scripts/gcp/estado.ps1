# Muestra el estado actual del proyecto en GCP (no cambia nada).
# Uso:  .\scripts\gcp\estado.ps1             (todo, una vez)
#       .\scripts\gcp\estado.ps1 -Cada 15    (solo MIGs y balanceador, repetido cada 15 s; Ctrl+C para salir)
# Por MIG muestra la politica de autoescalado efectiva, el tamano recomendado
# por el autoscaler (su decision) y sus avisos, p. ej. si la metrica no tiene datos.

param([int]$Cada = 0)

. "$PSScriptRoot\config.ps1"
Initialize-Gcp

function Show-Mig([string]$Mig) {
    Write-Step $Mig
    $tam = Get-G @('compute', 'instance-groups', 'managed', 'describe', $Mig, "--project=$Project", "--zone=$script:Zone", '--format=value(targetSize)')
    $as = Get-MigAutoscaler $Mig
    $recomendado = if ($as) { $as.recommendedSize } else { '-' }
    Write-Host "    tamano objetivo=$tam  recomendado por el autoscaler=$recomendado"
    Write-Host "    politica: $(Format-AutoscalingPolicy $as)"
    foreach ($d in @($as.statusDetails)) {
        if ($d -and $d.message) { Write-Warn "aviso del autoscaler: $($d.message)" }
    }
    Invoke-G @('compute', 'instance-groups', 'managed', 'list-instances', $Mig, "--project=$Project", "--zone=$script:Zone", '--format=table(name,status,currentAction,instanceHealth[0].detailedHealthState)') -AllowFail
}

function Show-Balanceador([string]$Ip) {
    Write-Step "Balanceador https://$Ip"
    $headers = & curl.exe -sk -m 8 -D - -o NUL "https://$Ip/health"
    # -match y -replace no distinguen mayusculas: con HTTP/2 las cabeceras llegan en minusculas.
    $status = "$($headers | Select-Object -First 1)".Trim()
    $servedBy = "$(($headers | Where-Object { $_ -match '^X-Served-By:' }) -replace '^X-Served-By:\s*', '')".Trim()
    $version = "$(($headers | Where-Object { $_ -match '^X-App-Version:' }) -replace '^X-App-Version:\s*', '')".Trim()
    Write-Host "    /health -> $status  instancia=$servedBy  version=$version"
}

$ip = Get-LbIp

if ($Cada -gt 0) {
    while ($true) {
        Write-Host ""
        Write-Host ("===== {0} =====" -f (Get-Date -Format 'yyyy-MM-dd HH:mm:ss')) -ForegroundColor Magenta
        foreach ($mig in @($ApiMig, $WorkerMig)) { Show-Mig $mig }
        if ($ip) { Show-Balanceador $ip }
        Start-Sleep -Seconds $Cada
    }
}

Write-Step 'Cloud SQL'
Write-Host ("    politica={0} estado={1}" -f (Get-SqlPolicy), (Get-SqlState))

Write-Step 'Redis'
Write-Host ("    {0}: {1}" -f $script:RedisVm, (Get-G @('compute', 'instances', 'describe', $script:RedisVm, "--project=$Project", "--zone=$script:Zone", '--format=value(status)')))

foreach ($mig in @($ApiMig, $WorkerMig)) { Show-Mig $mig }

if ($ip) { Show-Balanceador $ip }

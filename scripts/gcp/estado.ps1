# Muestra el estado actual del proyecto en GCP (no cambia nada).
# Uso:  .\scripts\gcp\estado.ps1

. "$PSScriptRoot\config.ps1"
Initialize-Gcp

Write-Step 'Cloud SQL'
Write-Host ("    politica={0} estado={1}" -f (Get-SqlPolicy), (Get-SqlState))

Write-Step 'Redis'
Write-Host ("    {0}: {1}" -f $script:RedisVm, (Get-G @('compute', 'instances', 'describe', $script:RedisVm, "--project=$Project", "--zone=$script:Zone", '--format=value(status)')))

foreach ($mig in @($ApiMig, $WorkerMig)) {
    Write-Step $mig
    $tam = Get-G @('compute', 'instance-groups', 'managed', 'describe', $mig, "--project=$Project", "--zone=$script:Zone", '--format=value(targetSize)')
    $modo = Get-G @('compute', 'instance-groups', 'managed', 'describe', $mig, "--project=$Project", "--zone=$script:Zone", '--format=value(status.autoscaler)')
    Write-Host "    tamano objetivo=$tam  autoscaler=$(if ($modo) { 'si' } else { 'no' })"
    Invoke-G @('compute', 'instance-groups', 'managed', 'list-instances', $mig, "--project=$Project", "--zone=$script:Zone", '--format=table(name,status,currentAction,instanceHealth[0].detailedHealthState)') -AllowFail
}

$ip = Get-LbIp
if ($ip) {
    Write-Step "Balanceador https://$ip"
    $code = (& curl.exe -sk -m 8 -o NUL -w '%{http_code}' "https://$ip/health")
    Write-Host "    /health -> HTTP $code"
}

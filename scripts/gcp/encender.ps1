# Enciende el proyecto en GCP: Cloud SQL, VM de Redis y autoescalado (1-3) de los MIGs de API y worker.
# Uso (PowerShell, desde la raiz del repo):  .\scripts\gcp\encender.ps1
# Primero se enciende la base de datos porque es lo que mas tarda; mientras tanto arranca Redis.
# Los MIGs arrancan cuando la base ya esta lista (la API aplica migraciones al iniciar y se cae si no hay base).
# Al final muestra cuanto tardo cada fase y el tiempo total hasta que todo respondio.

. "$PSScriptRoot\config.ps1"
$inicio = Get-Date
$fases = [ordered]@{}

function Format-Dur([TimeSpan]$t) { '{0} min {1:00} s' -f [int][math]::Floor($t.TotalMinutes), $t.Seconds }

Initialize-Gcp

Write-Step "1/4 Encendiendo Cloud SQL ($SqlInstance) y la VM de Redis ($script:RedisVm)"
if ((Get-SqlPolicy) -eq 'ALWAYS') {
    Write-Ok 'Cloud SQL ya estaba encendida.'
} else {
    Invoke-G @('sql', 'instances', 'patch', $SqlInstance, "--project=$Project", '--activation-policy=ALWAYS', '--async', '--quiet')
    Write-Ok 'Orden a Cloud SQL enviada; sigue en segundo plano.'
}
Invoke-G @('compute', 'instances', 'start', $script:RedisVm, "--project=$Project", "--zone=$script:Zone", '--quiet')

Write-Step '2/4 Esperando a que la base de datos este lista'
$limite = (Get-Date).AddMinutes(20)
while ($true) {
    $politica = Get-SqlPolicy
    $estado = Get-SqlState
    $pendientes = Get-SqlPendingOps
    Write-Host ("    politica={0} estado={1} operaciones_pendientes={2}  (transcurrido {3})" -f $politica, $estado, [bool]$pendientes, (Format-Dur ((Get-Date) - $inicio)))
    if ($politica -eq 'ALWAYS' -and $estado -eq 'RUNNABLE' -and -not $pendientes) { break }
    if ((Get-Date) -gt $limite) { throw 'La base de datos no quedo lista en 20 minutos. Revisa la consola de Cloud SQL.' }
    Start-Sleep -Seconds 15
}
Write-Ok 'Base de datos lista.'
$fases['Base de datos y Redis'] = (Get-Date) - $inicio

Write-Step "3/4 Activando el autoescalado ($MinReplicas-$MaxReplicas, CPU $TargetCpu) y esperando a que los MIGs queden estables"
$t = Get-Date
foreach ($mig in @($ApiMig, $WorkerMig)) {
    Invoke-G @('compute', 'instance-groups', 'managed', 'set-autoscaling', $mig, "--project=$Project", "--zone=$script:Zone",
        "--min-num-replicas=$MinReplicas", "--max-num-replicas=$MaxReplicas",
        "--target-cpu-utilization=$TargetCpu", "--cool-down-period=$CoolDown", '--mode=on')
    Write-Ok "$mig configurado."
}
foreach ($mig in @($ApiMig, $WorkerMig)) {
    Invoke-G @('compute', 'instance-groups', 'managed', 'wait-until', $mig, '--stable', "--project=$Project", "--zone=$script:Zone", '--timeout=900')
    Write-Ok "$mig estable (transcurrido $(Format-Dur ((Get-Date) - $inicio)))."
}
$fases['MIGs de API y worker'] = (Get-Date) - $t

$funcional = $false
$ip = Get-LbIp
if ($ip) {
    Write-Step "4/4 Esperando a que el balanceador responda en https://$ip/health"
    $t = Get-Date
    for ($i = 1; $i -le 30 -and -not $funcional; $i++) {
        $code = (& curl.exe -sk -m 8 -o NUL -w '%{http_code}' "https://$ip/health")
        Write-Host "    intento ${i}: HTTP $code  (transcurrido $(Format-Dur ((Get-Date) - $inicio)))"
        if ($code -eq '200') { $funcional = $true } else { Start-Sleep -Seconds 15 }
    }
    $fases['Balanceador'] = (Get-Date) - $t
} else {
    Write-Warn 'No encontre la IP del balanceador; omito la comprobacion final.'
}

$total = (Get-Date) - $inicio
Write-Host ""
Write-Host '--- Tiempos ---' -ForegroundColor Cyan
foreach ($k in $fases.Keys) { Write-Host ('    {0,-24} {1}' -f $k, (Format-Dur $fases[$k])) }
if ($funcional) {
    Write-Host ("Todo funcional en {0}  ->  https://{1}" -f (Format-Dur $total), $ip) -ForegroundColor Green
} else {
    Write-Host ("Terminado en {0}, pero el balanceador aun no responde 200. Revisa la salud de los backends en la consola." -f (Format-Dur $total)) -ForegroundColor Yellow
}

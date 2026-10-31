# Instala o actualiza el exportador de metricas de la cola en la VM de Redis
# (copia y ejecuta alli deploy/scripts/queue-metrics.sh), con la misma imagen
# que usan los MIG. Basta correrlo una vez: el contenedor vuelve a arrancar
# solo cada vez que se enciende la VM de Redis.
# Uso:  .\scripts\gcp\exportador-cola.ps1 [-Iap] [-Image <imagen:tag>]
#   -Iap    conecta por IAP (si la VM de Redis no tiene IP externa)
#   -Image  imagen a usar (por defecto, el metadato mooc-image de la plantilla de la API)

param([switch]$Iap, [string]$Image)

. "$PSScriptRoot\config.ps1"
Initialize-Gcp

if (-not $Image) {
    $tpl = Get-GcpJson (Get-MigTemplateUrl $ApiMig)
    $Image = Get-MetadataValue $tpl.properties 'mooc-image'
    if (-not $Image) { throw "La plantilla de $ApiMig no tiene el metadato mooc-image; pasa la imagen con -Image." }
}
Write-Host "Imagen: $Image"

$extra = @('--quiet', '--strict-host-key-checking=no')
if ($Iap) { $extra += '--tunnel-through-iap' }

Write-Step "1/2 Copiando queue-metrics.sh a $script:RedisVm"
# Ruta relativa: con "D:\..." gcloud scp confundiria la unidad con el nombre de una VM.
Push-Location (Join-Path $PSScriptRoot '..\..\deploy\scripts')
try {
    Invoke-G (@('compute', 'scp', 'queue-metrics.sh', "$($script:RedisVm):/tmp/queue-metrics.sh",
        "--zone=$script:Zone", "--project=$Project") + $extra)
} finally {
    Pop-Location
}

Write-Step '2/2 Arrancando el exportador (al final deben verse lineas "[QUEUE-METRICS] backlog=...")'
Invoke-G (@('compute', 'ssh', $script:RedisVm, "--zone=$script:Zone", "--project=$Project",
    "--command=sudo bash /tmp/queue-metrics.sh $Image") + $extra)

Write-Host ""
Write-Ok 'Listo. En 1-2 minutos la metrica aparece en Metrics Explorer; despues corre aplicar-autoscaling.ps1.'

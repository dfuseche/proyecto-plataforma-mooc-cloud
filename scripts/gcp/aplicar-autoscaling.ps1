# Aplica la politica de autoescalado de cada MIG con el proyecto ya encendido:
# API por CPU y workers por la cola (parametros en config.ps1). encender.ps1
# hace lo mismo en su paso 3; este script sirve para no tener que apagar y
# encender, por ejemplo justo despues de instalar el exportador de la cola.
# Uso:  .\scripts\gcp\aplicar-autoscaling.ps1

. "$PSScriptRoot\config.ps1"
Initialize-Gcp

# La politica de workers se puede crear sin la metrica, pero el autoscaler no
# decide nada hasta que el exportador la publique.
$descriptor = "https://monitoring.googleapis.com/v3/projects/$Project/metricDescriptors/$WorkerBacklogMetric"
try {
    Get-GcpJson $descriptor | Out-Null
    Write-Ok "La metrica $WorkerBacklogMetric existe en Cloud Monitoring."
} catch {
    Write-Warn "La metrica $WorkerBacklogMetric aun no existe: instala el exportador (exportador-cola.ps1) y espera 1-2 minutos."
}

Write-Step "Politica de $ApiMig (CPU $TargetCpu)"
Set-ApiAutoscaling
Write-Ok (Format-AutoscalingPolicy (Get-MigAutoscaler $ApiMig))

Write-Step "Politica de $WorkerMig (cola: $WorkerJobsPerVm trabajos por VM)"
Set-WorkerAutoscaling
Write-Ok (Format-AutoscalingPolicy (Get-MigAutoscaler $WorkerMig))

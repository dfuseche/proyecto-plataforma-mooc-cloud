<#
.SYNOPSIS
  Corre el Escenario 2 (carga, procesamiento y consumo multimedia) en
  varios niveles de carga separados, como exige la Entrega 2: una linea
  base con carga baja, al menos tres niveles crecientes, y una repeticion
  cerca del limite para comprobar estabilidad.

.DESCRIPTION
  Cada nivel es una corrida independiente de media-load-test.js, con su
  propio --summary-export a un JSON separado en loadtests/results/escenario2/.
  Sube UPLOAD_VUS y PLAYBACK_VUS juntos (mas subida + mas consumo
  concurrente), manteniendo fija la concurrencia de los workers (no se
  toca infraestructura entre niveles) y PLAYBACK_POOL_SIZE=3 (los 3
  perfiles de video exigidos por el enunciado) en todos los niveles.
  Corre esto DESDE TU MAQUINA (fuera de las dos VMs de la aplicacion).

.EXAMPLE
  ./run_escenario2_niveles.ps1 -BaseUrl http://35.209.105.199
#>

param(
  [Parameter(Mandatory = $true)][string]$BaseUrl,
  [switch]$RepetirUltimoNivel = $true,
  [int]$PausaEntreCorridasSegundos = 30
)

$ErrorActionPreference = "Stop"
$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$repoRoot = Split-Path -Parent (Split-Path -Parent $scriptDir)
$resultsDir = Join-Path $repoRoot "loadtests/results/escenario2"
New-Item -ItemType Directory -Force -Path $resultsDir | Out-Null

# etiqueta, UPLOAD_VUS, PLAYBACK_VUS, UPLOAD_DURATION, PLAYBACK_DURATION
$niveles = @(
  @{ tag = "00_linea_base";  upload = 1;  playback = 10;  dur = "2m" },
  @{ tag = "01_nivel1";      upload = 3;  playback = 50;  dur = "3m" },
  @{ tag = "02_nivel2";      upload = 5;  playback = 150; dur = "3m" },
  @{ tag = "03_nivel3";      upload = 10; playback = 300; dur = "3m" }
)
if ($RepetirUltimoNivel) {
  $ultimo = $niveles[-1].Clone()
  $ultimo.tag = "04_repeticion_cerca_del_limite"
  $niveles += $ultimo
}

$timestamp = Get-Date -Format "yyyyMMdd_HHmmss"

# Ventanas exactas de cada nivel (UTC) para cruzarlas con los CSV de
# monitor_vm.sh (Web Server / Worker Server) sin tener que inferirlas.
$ventanasCsv = Join-Path $resultsDir "$timestamp`_ventanas.csv"
"nivel,inicio_utc,fin_utc" | Out-File -FilePath $ventanasCsv -Encoding ascii

Write-Host "=== Escenario 2: $($niveles.Count) corridas contra $BaseUrl ===" -ForegroundColor Cyan

for ($idx = 0; $idx -lt $niveles.Count; $idx++) {
  $n = $niveles[$idx]
  $jsonOut = Join-Path $resultsDir "$timestamp`_$($n.tag)_up$($n.upload)_pb$($n.playback).json"
  $logOut = Join-Path $resultsDir "$timestamp`_$($n.tag)_up$($n.upload)_pb$($n.playback).log"

  Write-Host ""
  Write-Host "--- [$($idx + 1)/$($niveles.Count)] $($n.tag): UPLOAD_VUS=$($n.upload) PLAYBACK_VUS=$($n.playback) DURATION=$($n.dur) ---" -ForegroundColor Yellow

  $inicioUtc = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")
  k6 run `
    -e BASE_URL=$BaseUrl `
    -e UPLOAD_VUS=$($n.upload) `
    -e PLAYBACK_VUS=$($n.playback) `
    -e UPLOAD_DURATION=$($n.dur) `
    -e PLAYBACK_DURATION=$($n.dur) `
    -e PLAYBACK_POOL_SIZE=3 `
    --summary-export=$jsonOut `
    (Join-Path $scriptDir "media-load-test.js") 2>&1 | Tee-Object -FilePath $logOut

  $finUtc = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")
  "$($n.tag),$inicioUtc,$finUtc" | Out-File -FilePath $ventanasCsv -Encoding ascii -Append
  Write-Host "Resultado guardado en: $jsonOut" -ForegroundColor Green

  if ($idx -lt $niveles.Count - 1) {
    Write-Host "Pausa de $PausaEntreCorridasSegundos s antes del siguiente nivel (deja drenar cola/conexiones)..." -ForegroundColor DarkGray
    Start-Sleep -Seconds $PausaEntreCorridasSegundos
  }
}

Write-Host ""
Write-Host "=== Listo. Resultados en $resultsDir (ventanas por nivel: $ventanasCsv) ===" -ForegroundColor Cyan

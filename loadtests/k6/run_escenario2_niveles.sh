#!/usr/bin/env bash
#
# Corre el Escenario 2 (carga, procesamiento y consumo multimedia HLS) en
# varios niveles de carga separados, apto para ejecutarse en la VM generadora
# de carga (Linux) contra el Load Balancer HTTPS de la Entrega 3.
#
# Niveles por defecto:
#   - Línea base:  1 upload VU,   10 playback VUs, 2m duración
#   - Nivel 1:     3 upload VUs,  50 playback VUs, 3m duración
#   - Nivel 2:     5 upload VUs, 150 playback VUs, 3m duración
#   - Nivel 3:    10 upload VUs, 300 playback VUs, 3m duración
#   - Repetición: 10 upload VUs, 300 playback VUs, 3m duración
#
# Uso:
#   ./run_escenario2_niveles.sh -b "https://<IP_O_DOMINIO_LOAD_BALANCER>"
#   ./run_escenario2_niveles.sh -b "https://34.x.x.x" -p 45
#

set -euo pipefail

BASE_URL=""
PAUSE_SECONDS=30
REPETIR_ULTIMO=true

usage() {
  echo "Uso: $0 -b <BASE_URL> [-p <segundos_pausa>] [--no-repeat]"
  echo "  -b, --base-url    URL base del Load Balancer HTTPS (ej: https://34.120.x.x)"
  echo "  -p, --pause       Pausa entre corridas en segundos (default: 30)"
  echo "  --no-repeat       No repetir el último nivel de carga"
  echo "  -h, --help        Muestra esta ayuda"
  exit 1
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    -b|--base-url)
      BASE_URL="$2"
      shift 2
      ;;
    -p|--pause)
      PAUSE_SECONDS="$2"
      shift 2
      ;;
    --no-repeat)
      REPETIR_ULTIMO=false
      shift
      ;;
    -h|--help)
      usage
      ;;
    *)
      echo "Argumento desconocido: $1" >&2
      usage
      ;;
  esac
done

if [[ -z "$BASE_URL" ]]; then
  echo "Error: Se requiere el parámetro -b/--base-url." >&2
  usage
fi

BASE_URL="${BASE_URL%/}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
RESULTS_DIR="${REPO_ROOT}/loadtests/results/escenario2"
mkdir -p "${RESULTS_DIR}"

TIMESTAMP="$(date +'%Y%m%d_%H%M%S')"

# Definición de niveles: tag, upload_vus, playback_vus, duration
NIVELES_TAG=("00_linea_base" "01_nivel1" "02_nivel2" "03_nivel3")
NIVELES_UP=(1 3 5 10)
NIVELES_PB=(10 50 150 300)
NIVELES_DUR=("2m" "3m" "3m" "3m")

if [[ "$REPETIR_ULTIMO" == "true" ]]; then
  NIVELES_TAG+=("04_repeticion_cerca_del_limite")
  NIVELES_UP+=(10)
  NIVELES_PB+=(300)
  NIVELES_DUR+=("3m")
fi

TOTAL=${#NIVELES_TAG[@]}

VENTANAS_CSV="${RESULTS_DIR}/${TIMESTAMP}_ventanas.csv"
echo "nivel,inicio_utc,fin_utc" > "${VENTANAS_CSV}"

echo "====================================================================="
echo "=== Escenario 2: ${TOTAL} corridas contra ${BASE_URL} ==="
echo "====================================================================="

for ((idx=0; idx<TOTAL; idx++)); do
  tag="${NIVELES_TAG[idx]}"
  up="${NIVELES_UP[idx]}"
  pb="${NIVELES_PB[idx]}"
  dur="${NIVELES_DUR[idx]}"

  json_out="${RESULTS_DIR}/${TIMESTAMP}_${tag}_up${up}_pb${pb}.json"
  log_out="${RESULTS_DIR}/${TIMESTAMP}_${tag}_up${up}_pb${pb}.log"
  html_out="${RESULTS_DIR}/${TIMESTAMP}_${tag}_up${up}_pb${pb}_dashboard.html"

  echo ""
  echo "--- [$((idx + 1))/${TOTAL}] ${tag}: UPLOAD_VUS=${up} PLAYBACK_VUS=${pb} DURATION=${dur} ---"

  inicio_utc="$(date -u +'%Y-%m-%dT%H:%M:%SZ')"

  K6_WEB_DASHBOARD=true \
  K6_WEB_DASHBOARD_EXPORT="${html_out}" \
  K6_WEB_DASHBOARD_HOST=0.0.0.0 \
  K6_WEB_DASHBOARD_PORT=5665 \
  k6 run \
    --insecure-skip-tls-verify \
    -e BASE_URL="${BASE_URL}" \
    -e UPLOAD_VUS="${up}" \
    -e PLAYBACK_VUS="${pb}" \
    -e UPLOAD_DURATION="${dur}" \
    -e PLAYBACK_DURATION="${dur}" \
    -e PLAYBACK_POOL_SIZE=3 \
    --summary-export="${json_out}" \
    "${SCRIPT_DIR}/media-load-test.js" 2>&1 | tee "${log_out}"

  fin_utc="$(date -u +'%Y-%m-%dT%H:%M:%SZ')"
  echo "${tag},${inicio_utc},${fin_utc}" >> "${VENTANAS_CSV}"

  echo "Resultado JSON: ${json_out}"
  echo "Dashboard HTML: ${html_out}"

  if (( idx < TOTAL - 1 )); then
    echo "Pausa de ${PAUSE_SECONDS}s antes del siguiente nivel (drenado de cola y conexiones)..."
    sleep "${PAUSE_SECONDS}"
  fi
done

echo ""
echo "=== Escenario 2 completado. Resultados en ${RESULTS_DIR} ==="
echo "Ventanas UTC guardadas en: ${VENTANAS_CSV}"

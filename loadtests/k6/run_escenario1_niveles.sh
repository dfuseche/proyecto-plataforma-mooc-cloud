#!/usr/bin/env bash
#
# Corre el Escenario 1 (actividad académica concurrente) en varios niveles
# de carga separados, apto para ejecutarse en la VM generadora de carga (Linux)
# contra el Load Balancer HTTPS de la Entrega 3.
#
# Niveles por defecto:
#   - Línea base: 10 VUs
#   - Nivel 1:    50 VUs
#   - Nivel 2:   150 VUs
#   - Nivel 3:   400 VUs
#   - Repetición: 400 VUs (comprobación de estabilidad)
#
# Uso:
#   ./run_escenario1_niveles.sh -b "https://<IP_O_DOMINIO_LOAD_BALANCER>"
#   ./run_escenario1_niveles.sh -b "https://34.x.x.x" -p 45
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

# Quitar barra final si la tiene
BASE_URL="${BASE_URL%/}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
RESULTS_DIR="${REPO_ROOT}/loadtests/results/escenario1"
mkdir -p "${RESULTS_DIR}"

TIMESTAMP="$(date +'%Y%m%d_%H%M%S')"

NIVELES=(10 50 150 400)
ETIQUETAS=("00_linea_base" "01_nivel1" "02_nivel2" "03_nivel3")

if [[ "$REPETIR_ULTIMO" == "true" ]]; then
  NIVELES+=(400)
  ETIQUETAS+=("04_repeticion_cerca_del_limite")
fi

TOTAL=${#NIVELES[@]}

echo "====================================================================="
echo "=== Escenario 1: ${TOTAL} corridas contra ${BASE_URL} ==="
echo "=== Niveles (MAX_VUS): ${NIVELES[*]} ==="
echo "====================================================================="

for ((idx=0; idx<TOTAL; idx++)); do
  vus="${NIVELES[idx]}"
  etiqueta="${ETIQUETAS[idx]}"

  # Escalar tamaño del pool de usuarios logueados (mínimo 20, máximo 300)
  pool_size=$(( vus * 2 ))
  if (( pool_size < 20 )); then
    pool_size=20
  elif (( pool_size > 300 )); then
    pool_size=300
  fi

  json_out="${RESULTS_DIR}/${TIMESTAMP}_${etiqueta}_vus${vus}.json"
  log_out="${RESULTS_DIR}/${TIMESTAMP}_${etiqueta}_vus${vus}.log"
  html_out="${RESULTS_DIR}/${TIMESTAMP}_${etiqueta}_vus${vus}_dashboard.html"

  echo ""
  echo "--- [$((idx + 1))/${TOTAL}] ${etiqueta} : MAX_VUS=${vus} POOL_SIZE=${pool_size} ---"

  K6_WEB_DASHBOARD=true \
  K6_WEB_DASHBOARD_EXPORT="${html_out}" \
  K6_WEB_DASHBOARD_HOST=0.0.0.0 \
  K6_WEB_DASHBOARD_PORT=5665 \
  k6 run \
    --insecure-skip-tls-verify \
    -e BASE_URL="${BASE_URL}" \
    -e MAX_VUS="${vus}" \
    -e POOL_SIZE="${pool_size}" \
    -e LEVEL_RUN=true \
    --summary-export="${json_out}" \
    "${SCRIPT_DIR}/load-test.js" 2>&1 | tee "${log_out}" || true

  echo "Resultado JSON: ${json_out}"
  echo "Dashboard HTML: ${html_out}"

  if (( idx < TOTAL - 1 )); then
    echo "Pausa de ${PAUSE_SECONDS}s antes del siguiente nivel (drenado de conexiones)..."
    sleep "${PAUSE_SECONDS}"
  fi
done

echo ""
echo "=== Escenario 1 completado. Resultados en ${RESULTS_DIR} ==="

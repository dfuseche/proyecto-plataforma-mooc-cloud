#!/usr/bin/env bash
# Linea de tiempo del autoscaling (Escenario 3, C14). Cada INTERVALO segundos
# agrega al CSV una fila por grupo (API y workers) con el tamano objetivo, la
# decision del autoscaler (recommendedSize), su estado y avisos, las instancias
# (nombre:estado:accion:salud) y, para la API, la salud de cada backend en el
# balanceador. Es la fuente de los hitos T0..T9 (guia, seccion 9.1).
#
# Correr en Cloud Shell o en un equipo con gcloud, NUNCA en las VMs de los
# grupos (el autoscaler las crea y las borra). Solo lee; no cambia nada.
#
# Uso:
#   ./timeline_autoscaling.sh [--out archivo.csv] [--interval segundos]
#
# Ejemplo (guia, secciones 6 y 7):
#   ./loadtests/monitoring/timeline_autoscaling.sh --out $OUT/timeline.csv --interval 15
#
# Variables opcionales: PROJECT, ZONE, API_MIG, WORKER_MIG, BACKEND.
# Dependencias: gcloud y curl; jq o python3 para leer el JSON de la API de
# autoscalers (Cloud Shell trae los dos). En Git Bash de Windows, gcloud
# necesita CLOUDSDK_PYTHON apuntando al Python que trae el SDK.
#
# Detener con Ctrl+C cuando termine el ciclo (el CSV queda completo).

set -uo pipefail

PROJECT="${PROJECT:-proyecto-plataforma-mooc}"
ZONE="${ZONE:-us-central1-a}"
API_MIG="${API_MIG:-mooc-api-mig}"
WORKER_MIG="${WORKER_MIG:-mooc-worker-mig}"
BACKEND="${BACKEND:-mooc-api-backend-service}"

OUT="timeline_$(date -u +%Y%m%d_%H%M%S).csv"
INTERVAL=15

while [ $# -gt 0 ]; do
  case "$1" in
    --out) OUT="$2"; shift 2 ;;
    --interval) INTERVAL="$2"; shift 2 ;;
    -h|--help) sed -n '2,23p' "$0"; exit 0 ;;
    *) echo "Argumento desconocido: $1" >&2; exit 1 ;;
  esac
done

if command -v jq >/dev/null 2>&1; then
  PARSER=jq
else
  PY="${PYTHON:-${CLOUDSDK_PYTHON:-python3}}"
  if ! "$PY" -c 'import json' >/dev/null 2>&1; then
    echo "Falta jq o python3 para leer la API de autoscalers." >&2
    exit 1
  fi
  PARSER=python
fi

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
trap 'echo ""; echo "Detenido. Linea de tiempo en $OUT"; exit 0' INT TERM

mkdir -p "$(dirname "$OUT")"
if [ ! -s "$OUT" ]; then
  echo "ts,grupo,target_size,current_size,recommended_size,autoscaler_status,autoscaler_avisos,instancias_total,instancias_running,instancias,backends_healthy,backends" > "$OUT"
fi

# Campo de texto entre comillas (los avisos del autoscaler pueden traer comas).
csv() { local s=${1//\"/\"\"}; printf '"%s"' "$s"; }

warn() { echo "$(date -u +%H:%M:%S) aviso: $*" >&2; }

# Token de la API REST de Compute: se renueva cada 10 min (dura 1 h).
TOKEN=""
TOKEN_TS=0
refresh_token() {
  local now
  now=$(date +%s)
  if [ -z "$TOKEN" ] || [ $((now - TOKEN_TS)) -gt 600 ]; then
    TOKEN=$(gcloud auth print-access-token 2>/dev/null) && TOKEN_TS=$now || warn "no se pudo obtener el token"
  fi
}

# Una linea "grupo|recommendedSize|status|avisos" por autoscaler de la zona.
parse_autoscalers() {
  if [ "$PARSER" = jq ]; then
    jq -r '.items[]? | [(.target | split("/") | last), (.recommendedSize // "" | tostring),
      (.status // ""), ([.statusDetails[]?.message] | join(" / "))] | join("|")' "$1"
  else
    "$PY" - "$1" <<'EOF'
import json, sys
with open(sys.argv[1]) as f:
    data = json.load(f)
for a in data.get("items", []):
    avisos = " / ".join(d.get("message", "") for d in a.get("statusDetails", []))
    print("|".join([a.get("target", "").rsplit("/", 1)[-1], str(a.get("recommendedSize", "")),
                    a.get("status", ""), avisos]))
EOF
  fi
}

echo "Proyecto $PROJECT, zona $ZONE. Escribiendo cada ${INTERVAL}s en $OUT (Ctrl+C para detener)..."

while true; do
  start_epoch=$(date +%s)
  ts=$(date -u +%Y-%m-%dT%H:%M:%SZ)
  refresh_token

  # Las cinco consultas van en paralelo para que la muestra tome pocos segundos.
  gcloud compute instance-groups managed list --project="$PROJECT" --zones="$ZONE" \
    --format='value[separator="|"](name,targetSize,size)' > "$TMP/migs" 2>"$TMP/migs.err" &
  curl -sf -m 20 -H "Authorization: Bearer $TOKEN" \
    "https://compute.googleapis.com/compute/v1/projects/$PROJECT/zones/$ZONE/autoscalers" > "$TMP/as.json" 2>/dev/null &
  for mig in "$API_MIG" "$WORKER_MIG"; do
    gcloud compute instance-groups managed list-instances "$mig" --project="$PROJECT" --zone="$ZONE" \
      --format='value[separator=":"](name,instanceStatus,currentAction,instanceHealth[0].detailedHealthState)' \
      > "$TMP/inst_$mig" 2>"$TMP/inst_$mig.err" &
  done
  gcloud compute backend-services get-health "$BACKEND" --global --project="$PROJECT" \
    --flatten='status.healthStatus[]' \
    --format='value[separator=":"](status.healthStatus.instance.basename(),status.healthStatus.healthState)' \
    > "$TMP/backends" 2>"$TMP/backends.err" &
  wait

  [ -s "$TMP/migs.err" ] && warn "instance-groups list: $(head -1 "$TMP/migs.err")"
  [ -s "$TMP/backends.err" ] && warn "get-health: $(head -1 "$TMP/backends.err")"
  autoscalers=""
  if [ -s "$TMP/as.json" ]; then
    autoscalers=$(parse_autoscalers "$TMP/as.json" 2>/dev/null) || warn "no se pudo leer la API de autoscalers"
  else
    warn "la API de autoscalers no respondio"
  fi

  resumen=""
  for mig in "$API_MIG" "$WORKER_MIG"; do
    IFS='|' read -r _ target current <<< "$(grep "^$mig|" "$TMP/migs" 2>/dev/null)"
    IFS='|' read -r _ recommended as_status avisos <<< "$(grep "^$mig|" <<< "$autoscalers")"
    [ -s "$TMP/inst_$mig.err" ] && warn "list-instances $mig: $(head -1 "$TMP/inst_$mig.err")"

    inst_total=$(grep -c . "$TMP/inst_$mig" 2>/dev/null || true)
    inst_running=$(grep -c ':RUNNING:NONE' "$TMP/inst_$mig" 2>/dev/null || true)
    instancias=$(paste -sd';' "$TMP/inst_$mig" 2>/dev/null)

    backends=""
    backends_healthy=""
    if [ "$mig" = "$API_MIG" ]; then
      backends=$(paste -sd';' "$TMP/backends" 2>/dev/null)
      backends_healthy=$(grep -c ':HEALTHY$' "$TMP/backends" 2>/dev/null || true)
    fi

    echo "$ts,$mig,${target:-},${current:-},${recommended:-},${as_status:-},$(csv "${avisos:-}"),${inst_total:-0},${inst_running:-0},$(csv "$instancias"),${backends_healthy},$(csv "$backends")" >> "$OUT"
    resumen+=" | $mig objetivo=${target:-?} recomendado=${recommended:-?} operativas=${inst_running:-0}"
    [ "$mig" = "$API_MIG" ] && resumen+=" sanas_lb=${backends_healthy:-0}"
  done
  echo "$ts$resumen"

  elapsed=$(( $(date +%s) - start_epoch ))
  [ "$elapsed" -lt "$INTERVAL" ] && sleep $((INTERVAL - elapsed))
done

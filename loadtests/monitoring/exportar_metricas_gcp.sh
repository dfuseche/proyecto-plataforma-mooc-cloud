#!/usr/bin/env bash
# Descarga de Cloud Monitoring las metricas de una corrida (Escenario 3, C14),
# alineadas a 60 s, para cruzarlas con k6 y con timeline_autoscaling.sh:
# CPU por instancia de API, workers y generador, tamano de los grupos, memoria
# (solo si hay Ops Agent), peticiones y latencia del balanceador, CPU y
# conexiones de Cloud SQL y las metricas de la cola (custom.googleapis.com/mooc/asynq/*).
# Una consulta sin datos (p. ej. la cola antes de instalar el exportador) deja
# su CSV vacio y no detiene el script.
#
# Uso:
#   ./exportar_metricas_gcp.sh --start 2026-11-05T14:00:00Z --end 2026-11-05T14:50:00Z [--out DIR] [--align 60]
#   ./exportar_metricas_gcp.sh --minutes 60 [--out DIR]      (ultimos 60 minutos)
#
# Ejemplo (guia, seccion 8):
#   ./loadtests/monitoring/exportar_metricas_gcp.sh --start $START --end $END --out $OUT/monitoring
#
# Salida en DIR:
#   <consulta>.csv          timestamp,serie,valor (timestamp = fin de la ventana de 60 s, UTC)
#   raw/<consulta>_pN.json  respuestas originales de la API, por pagina
#
# Variables opcionales: PROJECT, API_MIG, WORKER_MIG, GENERATOR_VM.
# Dependencias: gcloud (solo para el token), curl y jq; si no hay jq, usa
# python3 (Cloud Shell trae los dos). En Git Bash de Windows, gcloud necesita
# CLOUDSDK_PYTHON apuntando al Python que trae el SDK.

set -uo pipefail

PROJECT="${PROJECT:-proyecto-plataforma-mooc}"
API_MIG="${API_MIG:-mooc-api-mig}"
WORKER_MIG="${WORKER_MIG:-mooc-worker-mig}"
GENERATOR_VM="${GENERATOR_VM:-mooc-load-generator}"

START=""
END=""
MINUTES=""
ALIGN=60
OUT="metricas_gcp_$(date -u +%Y%m%d_%H%M%S)"

while [ $# -gt 0 ]; do
  case "$1" in
    --start) START="$2"; shift 2 ;;
    --end) END="$2"; shift 2 ;;
    --minutes) MINUTES="$2"; shift 2 ;;
    --align) ALIGN="$2"; shift 2 ;;
    --out) OUT="$2"; shift 2 ;;
    -h|--help) sed -n '2,26p' "$0"; exit 0 ;;
    *) echo "Argumento desconocido: $1" >&2; exit 1 ;;
  esac
done

if [ -n "$MINUTES" ]; then
  END="${END:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"
  START=$(date -u -d "$END - $MINUTES minutes" +%Y-%m-%dT%H:%M:%SZ)
fi
if [ -z "$START" ] || [ -z "$END" ]; then
  echo "Indica --start y --end (UTC, ISO 8601) o --minutes. Ver --help." >&2
  exit 1
fi

if command -v jq >/dev/null 2>&1; then
  PARSER=jq
else
  PY="${PYTHON:-${CLOUDSDK_PYTHON:-python3}}"
  if ! "$PY" -c 'import json' >/dev/null 2>&1; then
    echo "Falta jq o python3 para leer las respuestas de Cloud Monitoring." >&2
    exit 1
  fi
  PARSER=python
fi

TOKEN=$(gcloud auth print-access-token 2>/dev/null)
if [ -z "$TOKEN" ]; then
  echo "No se pudo obtener el token (gcloud auth login)." >&2
  exit 1
fi

mkdir -p "$OUT/raw"
API_URL="https://monitoring.googleapis.com/v3/projects/$PROJECT/timeSeries"

# consulta | filtro | alineador | reductor entre series | campos de agrupacion (separados por coma)
# CPU, memoria y red quedan por instancia (sin reductor); el balanceador y
# Cloud SQL se suman o se combinan en percentiles.
CONSULTAS=$(cat <<EOF
cpu_api|metric.type="compute.googleapis.com/instance/cpu/utilization" AND metric.labels.instance_name=starts_with("$API_MIG")|ALIGN_MEAN||
cpu_workers|metric.type="compute.googleapis.com/instance/cpu/utilization" AND metric.labels.instance_name=starts_with("$WORKER_MIG")|ALIGN_MEAN||
cpu_generador|metric.type="compute.googleapis.com/instance/cpu/utilization" AND metric.labels.instance_name="$GENERATOR_VM"|ALIGN_MEAN||
memoria_instancias|metric.type="agent.googleapis.com/memory/percent_used" AND metric.labels.state="used"|ALIGN_MEAN||
red_rx_instancias|metric.type="compute.googleapis.com/instance/network/received_bytes_count" AND (metric.labels.instance_name=starts_with("$API_MIG") OR metric.labels.instance_name=starts_with("$WORKER_MIG"))|ALIGN_RATE||
red_tx_instancias|metric.type="compute.googleapis.com/instance/network/sent_bytes_count" AND (metric.labels.instance_name=starts_with("$API_MIG") OR metric.labels.instance_name=starts_with("$WORKER_MIG"))|ALIGN_RATE||
grupos_tamano|metric.type="compute.googleapis.com/instance_group/size" AND (resource.labels.instance_group_name="$API_MIG" OR resource.labels.instance_group_name="$WORKER_MIG")|ALIGN_MEAN||
lb_peticiones_por_s|metric.type="loadbalancing.googleapis.com/https/request_count"|ALIGN_RATE|REDUCE_SUM|metric.labels.response_code_class
lb_latencia_p50_ms|metric.type="loadbalancing.googleapis.com/https/total_latencies"|ALIGN_DELTA|REDUCE_PERCENTILE_50|
lb_latencia_p95_ms|metric.type="loadbalancing.googleapis.com/https/total_latencies"|ALIGN_DELTA|REDUCE_PERCENTILE_95|
lb_latencia_p99_ms|metric.type="loadbalancing.googleapis.com/https/total_latencies"|ALIGN_DELTA|REDUCE_PERCENTILE_99|
cloudsql_cpu|metric.type="cloudsql.googleapis.com/database/cpu/utilization"|ALIGN_MEAN||
cloudsql_conexiones|metric.type="cloudsql.googleapis.com/database/postgresql/num_backends"|ALIGN_MEAN|REDUCE_SUM|resource.labels.database_id
cola|metric.type=starts_with("custom.googleapis.com/mooc/asynq/")|ALIGN_MEAN||
EOF
)

# Convierte una pagina de timeSeries.list en filas "timestamp","serie","valor".
# serie = ultimo segmento del tipo de metrica + etiquetas (sin project_id).
parse_page() {
  if [ "$PARSER" = jq ]; then
    jq -r '.timeSeries[]? as $s
      | ($s.metric.type | split("/") | last) as $m
      | ([($s.metric.labels // {} | to_entries[]),
          ($s.resource.labels // {} | to_entries[] | select(.key != "project_id"))]
         | map("\(.key)=\(.value)") | join(";")) as $l
      | $s.points[]?
      | [.interval.endTime, ($m + (if $l == "" then "" else ";" + $l end)),
         (.value.doubleValue // .value.int64Value // .value.boolValue // "" | tostring)]
      | @csv' "$1"
  else
    "$PY" - "$1" <<'EOF'
import csv, json, sys
with open(sys.argv[1]) as f:
    data = json.load(f)
w = csv.writer(sys.stdout, quoting=csv.QUOTE_ALL, lineterminator="\n")
for s in data.get("timeSeries", []):
    m = s["metric"]["type"].rsplit("/", 1)[-1]
    labels = list(s["metric"].get("labels", {}).items())
    labels += [(k, v) for k, v in s.get("resource", {}).get("labels", {}).items() if k != "project_id"]
    serie = ";".join([m] + ["%s=%s" % kv for kv in labels])
    for p in s.get("points", []):
        v = p.get("value", {})
        valor = v.get("doubleValue", v.get("int64Value", v.get("boolValue", "")))
        w.writerow([p["interval"]["endTime"], serie, valor])
EOF
  fi
}

echo "Proyecto $PROJECT. Ventana $START -> $END, alineada a ${ALIGN}s. Salida en $OUT"

while IFS='|' read -r nombre filtro alineador reductor agrupar; do
  [ -z "$nombre" ] && continue
  csv_out="$OUT/$nombre.csv"
  tmp_rows="$OUT/raw/.$nombre.rows"
  : > "$tmp_rows"

  args=(--data-urlencode "filter=$filtro"
        --data-urlencode "interval.startTime=$START"
        --data-urlencode "interval.endTime=$END"
        --data-urlencode "aggregation.alignmentPeriod=${ALIGN}s"
        --data-urlencode "aggregation.perSeriesAligner=$alineador")
  [ -n "$reductor" ] && args+=(--data-urlencode "aggregation.crossSeriesReducer=$reductor")
  if [ -n "$agrupar" ]; then
    IFS=',' read -ra campos <<< "$agrupar"
    for c in "${campos[@]}"; do args+=(--data-urlencode "aggregation.groupByFields=$c"); done
  fi

  page=1
  token_pagina=""
  estado="ok"
  while true; do
    raw="$OUT/raw/${nombre}_p${page}.json"
    extra=()
    [ -n "$token_pagina" ] && extra=(--data-urlencode "pageToken=$token_pagina")
    code=$(curl -sS -G -m 60 -o "$raw" -w '%{http_code}' -H "Authorization: Bearer $TOKEN" \
      "${args[@]}" ${extra[@]+"${extra[@]}"} "$API_URL" 2>/dev/null)
    if [ "$code" != "200" ]; then
      # 404 = la metrica aun no existe (p. ej. la cola sin exportador): CSV vacio.
      estado="HTTP $code: $(tr -d '\n' < "$raw" | grep -o '"message": *"[^"]*"' | head -1)"
      break
    fi
    parse_page "$raw" >> "$tmp_rows" || { estado="no se pudo leer $raw"; break; }
    if [ "$PARSER" = jq ]; then
      token_pagina=$(jq -r '.nextPageToken // ""' "$raw")
    else
      token_pagina=$("$PY" -c 'import json,sys; print(json.load(open(sys.argv[1])).get("nextPageToken", ""))' "$raw")
    fi
    [ -z "$token_pagina" ] && break
    page=$((page + 1))
  done

  echo "timestamp,serie,valor" > "$csv_out"
  sort "$tmp_rows" >> "$csv_out"
  filas=$(($(wc -l < "$csv_out") - 1))
  series=$(cut -d, -f2 "$tmp_rows" | sort -u | grep -c . || true)
  rm -f "$tmp_rows"
  printf '  %-22s %5d filas  %3d series  %s\n' "$nombre" "$filas" "$series" "$estado"
done <<< "$CONSULTAS"

echo "Listo: $OUT"

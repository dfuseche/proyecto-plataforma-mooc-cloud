#!/usr/bin/env bash
# Actividad 1.2 (Entrega 4): prueba de autoescalado del worker vía Pub/Sub.
#
# Crea un curso de prueba con N recursos de video REALES (existen en la DB)
# y les asocia el mismo archivo .mp4 subido a Cloud Storage. Luego llama
# en paralelo a complete-upload, que es el flujo real: el backend marca cada
# recurso como "processing" y publica un mensaje en mooc-tasks-topic.
# Como el worker tiene concurrency=1, cada mensaje necesita su propia
# instancia y Cloud Run se detiene en max-instances=3; el resto espera en
# Pub/Sub.
#
# Por qué no publicar mensajes inventados directo al topic: con resource_id
# y original_key que no existen, el worker falla en milisegundos (500),
# no alcanza a escalar y Pub/Sub reintenta esos mensajes indefinidamente.
#
# Uso (en Google Cloud Shell):
#   bash scripts/gcp/prueba-carga-worker.sh                 # genera un video de prueba con ffmpeg
#   VIDEO_FILE=mi-video.mp4 bash scripts/gcp/prueba-carga-worker.sh
# Variables opcionales: MOOC_EMAIL, MOOC_PASSWORD, NUM_TAREAS (default 6),
#                       BACKEND_URL, MEDIA_BUCKET
set -euo pipefail

PROJECT="${PROJECT:-proyecto-plataforma-mooc}"
BACKEND_URL="${BACKEND_URL:-https://mooc-backend-y3bzdi6bfa-uc.a.run.app}"
API="$BACKEND_URL/api/v1"
MEDIA_BUCKET="${MEDIA_BUCKET:-mooc-media-equipo-3}"
NUM_TAREAS="${NUM_TAREAS:-6}"
MOOC_EMAIL="${MOOC_EMAIL:-admin@mooc.com}"
STAMP=$(date -u +%Y%m%d-%H%M%S)
OBJECT_KEY="loadtest/$STAMP/original.mp4"

command -v jq >/dev/null 2>&1 || { echo "[ERROR] Se requiere jq" >&2; exit 1; }

if [ -z "${MOOC_PASSWORD:-}" ]; then
  read -r -s -p "Contraseña de $MOOC_EMAIL: " MOOC_PASSWORD; echo
fi

# ---------------------------------------------------------------------------
# 1. Video de prueba
# ---------------------------------------------------------------------------
if [ -z "${VIDEO_FILE:-}" ]; then
  VIDEO_FILE="/tmp/loadtest-video.mp4"
  if [ ! -f "$VIDEO_FILE" ]; then
    if ! command -v ffmpeg >/dev/null 2>&1; then
      echo ">>> Instalando ffmpeg (solo la primera vez)..."
      sudo apt-get update -qq && sudo apt-get install -y -qq ffmpeg >/dev/null
    fi
    echo ">>> Generando video de prueba 720p de 90 s..."
    ffmpeg -loglevel error -y \
      -f lavfi -i "testsrc2=size=1280x720:rate=30:duration=90" \
      -f lavfi -i "sine=frequency=440:duration=90" \
      -c:v libx264 -preset veryfast -pix_fmt yuv420p -c:a aac -shortest "$VIDEO_FILE"
  fi
fi
echo ">>> Subiendo $VIDEO_FILE a gs://$MEDIA_BUCKET/$OBJECT_KEY"
gcloud storage cp "$VIDEO_FILE" "gs://$MEDIA_BUCKET/$OBJECT_KEY" --project="$PROJECT" --quiet

# ---------------------------------------------------------------------------
# 2. Login y estructura del curso de prueba
# ---------------------------------------------------------------------------
api() {  # api METODO RUTA [JSON]
  local resp
  resp=$(curl -s -X "$1" "$API$2" -H "Authorization: Bearer ${TOKEN:-}" \
    -H "Content-Type: application/json" ${3:+-d "$3"})
  if [ "$(echo "$resp" | jq -r '.success')" != "true" ]; then
    echo "[ERROR] $1 $2 -> $resp" >&2; return 1
  fi
  echo "$resp"
}

echo ">>> Iniciando sesión como $MOOC_EMAIL..."
TOKEN=$(api POST /auth/login "$(jq -nc --arg e "$MOOC_EMAIL" --arg p "$MOOC_PASSWORD" '{email:$e,password:$p}')" \
  | jq -r '.data.session.token')

echo ">>> Creando curso de prueba..."
VERSION_ID=$(api POST /courses "$(jq -nc --arg t "Prueba carga worker $STAMP" '{title:$t,summary:"Curso generado por prueba-carga-worker.sh"}')" \
  | jq -r '.data.version.id')
MODULE_ID=$(api POST "/courses/versions/$VERSION_ID/modules" '{"title":"Modulo carga","position":1}' | jq -r '.data.id')
UNIT_ID=$(api POST "/courses/modules/$MODULE_ID/units" '{"title":"Unidad carga","position":1}' | jq -r '.data.id')

RESOURCE_IDS=()
for i in $(seq 1 "$NUM_TAREAS"); do
  RID=$(api POST "/courses/units/$UNIT_ID/resources" \
    "$(jq -nc --arg t "Video carga $i" --argjson p "$i" '{title:$t,type:"video",position:$p,is_visible:true}')" \
    | jq -r '.data.id')
  RESOURCE_IDS+=("$RID")
  echo "    Recurso $i: $RID"
done

# ---------------------------------------------------------------------------
# 3. Ráfaga: complete-upload en paralelo -> N mensajes en Pub/Sub
# ---------------------------------------------------------------------------
INICIO=$(date -u +%H:%M:%S)
echo ">>> [$INICIO UTC] Disparando $NUM_TAREAS tareas de transcodificación en paralelo..."
echo "    Abre ahora: Cloud Run -> mooc-worker -> Métricas -> 'Container instance count'"
for RID in "${RESOURCE_IDS[@]}"; do
  ( api POST "/media/resources/$RID/complete-upload" "$(jq -nc --arg k "$OBJECT_KEY" '{object_key:$k}')" >/dev/null \
      && echo "    -> publicado: $RID" ) &
done
wait

# ---------------------------------------------------------------------------
# 4. Seguimiento en Cloud Logging hasta que todos terminen
# ---------------------------------------------------------------------------
DESDE=$(date -u -d "-2 min" +%Y-%m-%dT%H:%M:%SZ)
echo ">>> Esperando a que el worker procese todo (Ctrl+C para salir sin afectar la prueba)..."
for _ in $(seq 1 60); do
  sleep 20
  LOGS=$(gcloud logging read "resource.type=\"cloud_run_revision\" AND resource.labels.service_name=\"mooc-worker\" AND timestamp>=\"$DESDE\" AND textPayload:\"WORKER-TASK\""     --project="$PROJECT" --limit=500 --format="value(textPayload)" 2>/dev/null || true)
  OK=0; ERR=0
  for RID in "${RESOURCE_IDS[@]}"; do
    echo "$LOGS" | grep "WORKER-TASK-SUCCESS" | grep -q "$RID" && OK=$((OK+1))
    echo "$LOGS" | grep "WORKER-TASK-ERROR" | grep -q "$RID" && ERR=$((ERR+1))
  done
  echo "    [$(date -u +%H:%M:%S)] completadas: $OK/$NUM_TAREAS  (recursos con algún error/reintento: $ERR)"
  [ "$OK" -ge "$NUM_TAREAS" ] && break
done

echo ""
echo ">>> Prueba terminada. Recursos usados (útiles para Actividad 2 / idempotencia):"
printf '    %s\n' "${RESOURCE_IDS[@]}"
echo "    Logs: gcloud logging read 'resource.labels.service_name=\"mooc-worker\" AND textPayload=~\"WORKER-TASK\"' --limit=30 --format='table(timestamp,textPayload)'"
echo "    Captura la gráfica y guárdala como docs/entrega4/evidencias/02-autoescalado-worker-max3.png"

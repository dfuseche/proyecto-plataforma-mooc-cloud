#!/usr/bin/env bash
# Actividad 1.1 (Entrega 4): prueba de carga del backend en Cloud Run.
# Genera 150 conexiones concurrentes durante 3 minutos contra un endpoint
# público para forzar el autoescalado de mooc-backend hasta su tope de 3
# instancias (concurrency=80, max-instances=3).
#
# Uso (en Google Cloud Shell):
#   bash scripts/gcp/prueba-carga-backend.sh
# Variables opcionales: BACKEND_URL, DURACION (ej. 3m), CONCURRENCIA (ej. 150)
set -euo pipefail

PROJECT="${PROJECT:-proyecto-plataforma-mooc}"
REGION="${REGION:-us-central1}"
BACKEND_URL="${BACKEND_URL:-https://mooc-backend-y3bzdi6bfa-uc.a.run.app}"
DURACION="${DURACION:-3m}"
CONCURRENCIA="${CONCURRENCIA:-150}"
ENDPOINT="$BACKEND_URL/api/v1/courses"

echo ">>> Verificando que Cloud SQL esté encendida..."
ESTADO_SQL=$(gcloud sql instances describe mooc-db-instance --project="$PROJECT" --format="value(settings.activationPolicy)" 2>/dev/null || echo "DESCONOCIDO")
if [ "$ESTADO_SQL" = "NEVER" ]; then
  echo "    Cloud SQL está apagada. Encendiéndola (tarda 1-3 min)..."
  gcloud sql instances patch mooc-db-instance --project="$PROJECT" --activation-policy=ALWAYS --quiet
fi

echo ">>> Instalando hey si no está disponible..."
if ! command -v hey >/dev/null 2>&1; then
  if command -v go >/dev/null 2>&1; then
    go install github.com/rakyll/hey@latest
    export PATH="$PATH:$(go env GOPATH)/bin"
  else
    wget -q https://hey-release.s3.us-east-2.amazonaws.com/hey_linux_amd64 -O /tmp/hey
    chmod +x /tmp/hey && sudo mv /tmp/hey /usr/local/bin/hey
  fi
fi

echo ">>> Comprobando que el endpoint responde..."
CODIGO=$(curl -s -o /dev/null -w "%{http_code}" "$ENDPOINT")
echo "    GET $ENDPOINT -> HTTP $CODIGO"
if [ "$CODIGO" != "200" ]; then
  echo "[ERROR] El backend no responde 200. Revisa el servicio antes de la prueba." >&2
  exit 1
fi

echo ">>> Instancias configuradas en mooc-backend:"
gcloud run services describe mooc-backend --project="$PROJECT" --region="$REGION" \
  --format="value(spec.template.metadata.annotations['autoscaling.knative.dev/maxScale'],spec.template.spec.containerConcurrency)" \
  | awk '{print "    max-instances=" $1 ", concurrency=" $2}'

INICIO=$(date -u +%H:%M:%S)
echo ">>> [$INICIO UTC] Lanzando $CONCURRENCIA conexiones concurrentes durante $DURACION..."
echo "    Abre ahora: Cloud Run -> mooc-backend -> Métricas -> 'Container instance count'"
hey -z "$DURACION" -c "$CONCURRENCIA" "$ENDPOINT" | tee /tmp/hey-backend-resultado.txt
FIN=$(date -u +%H:%M:%S)

echo ""
echo ">>> Prueba terminada ($INICIO -> $FIN UTC). Resultado guardado en /tmp/hey-backend-resultado.txt"
echo "    Las métricas de Cloud Run tardan 1-3 minutos en reflejarse."
echo "    Captura la gráfica y guárdala como docs/entrega4/evidencias/01-autoescalado-backend-max3.png"

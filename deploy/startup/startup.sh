#!/bin/bash
# Startup script para instancias de los MIGs (API y worker).
#
# Se pasa como metadata `startup-script` de la plantilla de instancia y corre
# en CADA arranque de la VM. Es el mismo script para los dos roles; lo que
# cambia se lee de metadata de la plantilla:
#   mooc-role    api | worker
#   mooc-secret  nombre del secreto de Secret Manager con el contenido del .env
#   mooc-image   imagen completa en Artifact Registry (con tag)
#
# La VM usa su cuenta de servicio (roles: artifactregistry.reader y
# secretmanager.secretAccessor sobre su propio secreto). No hay credenciales
# en el disco de la imagen ni en el repositorio.
set -euo pipefail

MD=http://metadata.google.internal/computeMetadata/v1
md() { curl -sf -H "Metadata-Flavor: Google" "$MD/$1"; }

ROLE=$(md instance/attributes/mooc-role)
SECRET=$(md instance/attributes/mooc-secret)
IMAGE=$(md instance/attributes/mooc-image)
PROJECT=$(md project/project-id)
REGISTRY=${IMAGE%%/*}

# Docker (solo la primera vez en una imagen base limpia)
if ! command -v docker >/dev/null 2>&1; then
  export DEBIAN_FRONTEND=noninteractive
  apt-get update -y
  apt-get install -y docker.io
fi
systemctl enable --now docker

# Token de la cuenta de servicio de la VM (sin gcloud)
TOKEN=$(md instance/service-accounts/default/token \
  | python3 -c 'import sys,json; print(json.load(sys.stdin)["access_token"])')

echo "$TOKEN" | docker login -u oauth2accesstoken --password-stdin "$REGISTRY"

# .env desde Secret Manager. Nota: `docker run --env-file` NO quita comillas,
# a diferencia de docker-compose; el secreto debe ir sin comillas (KEY=valor).
install -d -m 700 /opt/mooc
umask 077
curl -sf -H "Authorization: Bearer $TOKEN" \
  "https://secretmanager.googleapis.com/v1/projects/${PROJECT}/secrets/${SECRET}/versions/latest:access" \
  | python3 -c 'import sys,json,base64; sys.stdout.buffer.write(base64.b64decode(json.load(sys.stdin)["payload"]["data"]))' \
  > /opt/mooc/app.env

docker pull "$IMAGE"

case "$ROLE" in
  api)
    docker rm -f mooc-api-server 2>/dev/null || true
    docker run -d --name mooc-api-server --restart=always \
      -p 80:8080 -p 8080:8080 --env-file /opt/mooc/app.env "$IMAGE"
    ;;
  worker)
    docker rm -f mooc-worker-server 2>/dev/null || true
    docker run -d --name mooc-worker-server --restart=always \
      --env-file /opt/mooc/app.env "$IMAGE" /app/worker
    ;;
  *)
    echo "mooc-role desconocido: $ROLE" >&2
    exit 1
    ;;
esac

#!/bin/bash
# Autentica docker contra Artifact Registry usando la cuenta de servicio de la
# VM (token del servidor de metadata; no requiere gcloud). Lo usa el CD en las
# VMs actuales antes de `docker-compose pull`.
# Uso: bash deploy/scripts/gar-login.sh us-central1-docker.pkg.dev
set -euo pipefail
REGISTRY=${1:?uso: gar-login.sh <registry-host>}
TOKEN=$(curl -sf -H "Metadata-Flavor: Google" \
  http://metadata.google.internal/computeMetadata/v1/instance/service-accounts/default/token \
  | python3 -c 'import sys,json; print(json.load(sys.stdin)["access_token"])')
echo "$TOKEN" | docker login -u oauth2accesstoken --password-stdin "$REGISTRY"

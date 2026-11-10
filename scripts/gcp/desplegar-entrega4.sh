#!/usr/bin/env bash
# Script de Despliegue Automatizado para la Entrega 4: Cloud Run y Cloud Pub/Sub
# Diseñado para ejecutarse en Google Cloud Shell o Linux/macOS.
set -euo pipefail

PROJECT="${PROJECT:-proyecto-plataforma-mooc}"
REGION="${REGION:-us-central1}"
REPO_NAME="${REPO_NAME:-mooc-repo}"
SQL_INSTANCE="${SQL_INSTANCE:-mooc-db-instance}"
MEDIA_BUCKET="${MEDIA_BUCKET:-mooc-media-equipo-3}"
TOPIC_NAME="${TOPIC_NAME:-mooc-tasks-topic}"
SUB_NAME="${SUB_NAME:-mooc-worker-push-sub}"

echo ""
echo "========================================================"
echo ">>> 1. Configurando proyecto GCP: $PROJECT en región $REGION"
echo "========================================================"
gcloud config set project "$PROJECT"
PROJECT_NUMBER=$(gcloud projects describe "$PROJECT" --format="value(projectNumber)")
echo "[OK] Proyecto configurado. Número de proyecto: $PROJECT_NUMBER"

echo ""
echo "========================================================"
echo ">>> 2. Habilitando APIs requeridas"
echo "========================================================"
gcloud services enable \
    run.googleapis.com \
    pubsub.googleapis.com \
    artifactregistry.googleapis.com \
    secretmanager.googleapis.com \
    sqladmin.googleapis.com
echo "[OK] APIs habilitadas."

echo ""
echo "========================================================"
echo ">>> 3. Verificando repositorio en Artifact Registry"
echo "========================================================"
if ! gcloud artifacts repositories describe "$REPO_NAME" --location="$REGION" >/dev/null 2>&1; then
    echo "[INFO] Creando repositorio $REPO_NAME en $REGION..."
    gcloud artifacts repositories create "$REPO_NAME" \
        --repository-format=docker \
        --location="$REGION" \
        --description="Repositorio de contenedores Entrega 4"
fi
REPO_URL="$REGION-docker.pkg.dev/$PROJECT/$REPO_NAME"
echo "[OK] Artifact Registry listo: $REPO_URL"

echo ""
echo "========================================================"
echo ">>> 4. Configurando Cuentas de Servicio e IAM"
echo "========================================================"
BACKEND_SA="sa-mooc-backend"
WORKER_SA="sa-mooc-worker"
INVOKER_SA="sa-pubsub-invoker"

BACKEND_EMAIL="$BACKEND_SA@$PROJECT.iam.gserviceaccount.com"
WORKER_EMAIL="$WORKER_SA@$PROJECT.iam.gserviceaccount.com"
INVOKER_EMAIL="$INVOKER_SA@$PROJECT.iam.gserviceaccount.com"

for sa in "$BACKEND_SA" "$WORKER_SA" "$INVOKER_SA"; do
    if ! gcloud iam service-accounts describe "$sa@$PROJECT.iam.gserviceaccount.com" >/dev/null 2>&1; then
        echo "[INFO] Creando cuenta de servicio: $sa"
        gcloud iam service-accounts create "$sa" --display-name="Cuenta de servicio $sa"
    fi
done

# Permisos mínimos Backend
gcloud projects add-iam-policy-binding "$PROJECT" --member="serviceAccount:$BACKEND_EMAIL" --role="roles/cloudsql.client" --condition=None --quiet >/dev/null
gcloud projects add-iam-policy-binding "$PROJECT" --member="serviceAccount:$BACKEND_EMAIL" --role="roles/pubsub.publisher" --condition=None --quiet >/dev/null
gcloud projects add-iam-policy-binding "$PROJECT" --member="serviceAccount:$BACKEND_EMAIL" --role="roles/storage.objectAdmin" --condition=None --quiet >/dev/null
gcloud projects add-iam-policy-binding "$PROJECT" --member="serviceAccount:$BACKEND_EMAIL" --role="roles/secretmanager.secretAccessor" --condition=None --quiet >/dev/null

# Permisos mínimos Worker
gcloud projects add-iam-policy-binding "$PROJECT" --member="serviceAccount:$WORKER_EMAIL" --role="roles/cloudsql.client" --condition=None --quiet >/dev/null
gcloud projects add-iam-policy-binding "$PROJECT" --member="serviceAccount:$WORKER_EMAIL" --role="roles/storage.objectAdmin" --condition=None --quiet >/dev/null
gcloud projects add-iam-policy-binding "$PROJECT" --member="serviceAccount:$WORKER_EMAIL" --role="roles/secretmanager.secretAccessor" --condition=None --quiet >/dev/null

echo "[OK] Cuentas de servicio y permisos IAM configurados."

echo ""
echo "========================================================"
echo ">>> 5. Configurando Topic de Cloud Pub/Sub: $TOPIC_NAME"
echo "========================================================"
if ! gcloud pubsub topics describe "$TOPIC_NAME" >/dev/null 2>&1; then
    gcloud pubsub topics create "$TOPIC_NAME"
fi
echo "[OK] Topic listo: $TOPIC_NAME"

BACKEND_IMAGE="$REPO_URL/mooc-backend:latest"
WORKER_IMAGE="$REPO_URL/mooc-worker:latest"
FRONTEND_IMAGE="$REPO_URL/mooc-frontend:latest"

echo ""
echo "========================================================"
echo ">>> 6. Construyendo y publicando imágenes de Backend y Worker"
echo "========================================================"
gcloud auth configure-docker "$REGION-docker.pkg.dev" --quiet

echo "[INFO] Compilando imagen de Backend..."
docker build -t "$BACKEND_IMAGE" -f backend/Dockerfile.backend backend/
docker push "$BACKEND_IMAGE"

echo "[INFO] Compilando imagen de Worker..."
docker build -t "$WORKER_IMAGE" -f backend/Dockerfile.worker backend/
docker push "$WORKER_IMAGE"

echo ""
echo "========================================================"
echo ">>> 7. Desplegando Servicio Cloud Run: mooc-worker"
echo "========================================================"
gcloud run deploy mooc-worker \
    --image="$WORKER_IMAGE" \
    --region="$REGION" \
    --service-account="$WORKER_EMAIL" \
    --no-allow-unauthenticated \
    --max-instances=3 \
    --min-instances=0 \
    --concurrency=1 \
    --cpu=2 \
    --memory=2Gi \
    --timeout=600s \
    --add-cloudsql-instances="$PROJECT:$REGION:$SQL_INSTANCE" \
    --set-secrets="DATABASE_URL=mooc-db-url:latest,JWT_SECRET=mooc-jwt-secret:latest" \
    --set-env-vars="S3_REGION=$REGION,S3_MEDIA_BUCKET=$MEDIA_BUCKET,MINIO_ENDPOINT=storage.googleapis.com,MINIO_SECURE=true" \
    --quiet

WORKER_URL=$(gcloud run services describe mooc-worker --region="$REGION" --format="value(status.url)")
echo "[OK] Worker desplegado en: $WORKER_URL"

echo ""
echo "========================================================"
echo ">>> 8. Autorizando a la suscripción push a invocar mooc-worker"
echo "========================================================"
gcloud run services add-iam-policy-binding mooc-worker \
    --region="$REGION" \
    --member="serviceAccount:$INVOKER_EMAIL" \
    --role="roles/run.invoker" \
    --quiet >/dev/null
echo "[OK] Invocador autorizado."

echo ""
echo "========================================================"
echo ">>> 9. Configurando Suscripción Push: $SUB_NAME"
echo "========================================================"
PUSH_ENDPOINT="$WORKER_URL/pubsub/transcode"
if gcloud pubsub subscriptions describe "$SUB_NAME" >/dev/null 2>&1; then
    echo "[INFO] Actualizando endpoint de la suscripción push..."
    gcloud pubsub subscriptions modify-push-config "$SUB_NAME" \
        --push-endpoint="$PUSH_ENDPOINT" \
        --push-auth-service-account="$INVOKER_EMAIL"
else
    echo "[INFO] Creando suscripción push autenticada..."
    gcloud pubsub subscriptions create "$SUB_NAME" \
        --topic="$TOPIC_NAME" \
        --push-endpoint="$PUSH_ENDPOINT" \
        --push-auth-service-account="$INVOKER_EMAIL" \
        --ack-deadline=600 \
        --min-retry-delay=10s \
        --max-retry-delay=300s
fi
echo "[OK] Suscripción Push configurada apuntando a: $PUSH_ENDPOINT"

echo ""
echo "========================================================"
echo ">>> 10. Desplegando Servicio Cloud Run: mooc-backend"
echo "========================================================"
gcloud run deploy mooc-backend \
    --image="$BACKEND_IMAGE" \
    --region="$REGION" \
    --service-account="$BACKEND_EMAIL" \
    --allow-unauthenticated \
    --max-instances=3 \
    --min-instances=0 \
    --concurrency=80 \
    --cpu=1 \
    --memory=1Gi \
    --timeout=60s \
    --add-cloudsql-instances="$PROJECT:$REGION:$SQL_INSTANCE" \
    --set-secrets="DATABASE_URL=mooc-db-url:latest,JWT_SECRET=mooc-jwt-secret:latest" \
    --set-env-vars="GCP_PROJECT=$PROJECT,PUBSUB_TOPIC=$TOPIC_NAME,S3_REGION=$REGION,S3_MEDIA_BUCKET=$MEDIA_BUCKET,MINIO_ENDPOINT=storage.googleapis.com,MINIO_SECURE=true" \
    --quiet

BACKEND_URL=$(gcloud run services describe mooc-backend --region="$REGION" --format="value(status.url)")
echo "[OK] Backend desplegado en: $BACKEND_URL"

echo ""
echo "========================================================"
echo ">>> 11. Compilando y desplegando Servicio Cloud Run: mooc-frontend"
echo "========================================================"
echo "[INFO] Construyendo Frontend inyectando VITE_API_URL=$BACKEND_URL..."
docker build \
    --build-arg VITE_API_URL="$BACKEND_URL" \
    -t "$FRONTEND_IMAGE" \
    -f frontend/Dockerfile frontend/
docker push "$FRONTEND_IMAGE"

gcloud run deploy mooc-frontend \
    --image="$FRONTEND_IMAGE" \
    --region="$REGION" \
    --allow-unauthenticated \
    --max-instances=3 \
    --min-instances=0 \
    --concurrency=80 \
    --cpu=1 \
    --memory=512Mi \
    --timeout=60s \
    --quiet

FRONTEND_URL=$(gcloud run services describe mooc-frontend --region="$REGION" --format="value(status.url)")
echo "[OK] Frontend desplegado en: $FRONTEND_URL"

echo ""
echo "========================================================"
echo ">>> 12. Configurando CORS en Backend para permitir el Frontend"
echo "========================================================"
gcloud run services update mooc-backend \
    --region="$REGION" \
    --update-env-vars="CORS_ALLOWED_ORIGINS=$FRONTEND_URL" \
    --quiet
echo "[OK] CORS configurado para origen: $FRONTEND_URL"

echo ""
echo "========================================================"
echo "DESPLIEGUE DE LA ENTREGA 4 COMPLETADO EXITOSAMENTE"
echo "========================================================"
echo "Frontend URL : $FRONTEND_URL"
echo "Backend URL  : $BACKEND_URL"
echo "Worker URL   : $WORKER_URL (Privado, máx 3 instancias)"
echo "Topic PubSub : $TOPIC_NAME"
echo "Push Sub     : $SUB_NAME"
echo ""
echo "Para apagar las VMs anteriores y ahorrar costos:"
echo "  ./scripts/gcp/apagar.ps1 (o desde la consola de Compute Engine)"

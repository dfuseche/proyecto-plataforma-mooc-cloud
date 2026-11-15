# Guía Reproducible de Despliegue — Entrega 4: Cloud Run y Cloud Pub/Sub

Esta guía describe el procedimiento paso a paso para reproducir el despliegue de los tres servicios independientes en Google Cloud Run y la integración con Cloud Pub/Sub, Cloud SQL y Cloud Storage.

---

## 1. Prerrequisitos

- **Google Cloud SDK (`gcloud`)** instalado y autenticado con `gcloud auth login`.
- **Docker** instalado y en ejecución localmente (o uso de Cloud Build).
- Acceso al proyecto de GCP con el rol `Owner` o `Editor` + `Security Admin`.

Variables del entorno utilizadas durante el despliegue:
```bash
export PROJECT_ID="proyecto-plataforma-mooc"
export REGION="us-central1"
export REPO_NAME="mooc-repo"
export SQL_INSTANCE="mooc-db-instance"
export MEDIA_BUCKET="mooc-media-equipo-3"
export TOPIC_ID="mooc-tasks-topic"
export SUB_ID="mooc-worker-push-sub"
export REPO="$REGION-docker.pkg.dev/$PROJECT_ID/$REPO_NAME"
```

---

## 2. Habilitación de APIs en GCP

```bash
gcloud config set project $PROJECT_ID

gcloud services enable \
  run.googleapis.com \
  pubsub.googleapis.com \
  artifactregistry.googleapis.com \
  secretmanager.googleapis.com \
  sqladmin.googleapis.com
```

---

## 3. Repositorio de Contenedores (Artifact Registry)

Crear el repositorio si no existe:
```bash
gcloud artifacts repositories create $REPO_NAME \
  --repository-format=docker \
  --location=$REGION \
  --description="Imágenes Docker para Plataforma MOOC Entrega 4"
```

Configurar credenciales locales de Docker para Artifact Registry:
```bash
gcloud auth configure-docker "$REGION-docker.pkg.dev" --quiet
```

---

## 4. Cuentas de Servicio con Mínimos Privilegios (IAM)

Se crean 3 cuentas de servicio dedicadas para cumplir el principio de mínimo privilegio:

1. **`sa-mooc-backend`**:
   - `roles/cloudsql.client`: Conexión proxy/socket hacia Cloud SQL.
   - `roles/pubsub.publisher`: Publicar tareas de transcodificación en el topic.
   - `roles/storage.objectAdmin`: Generar URLs prefirmadas para subida y descarga de videos.
   - `roles/secretmanager.secretAccessor`: Leer secretos de conexión y JWT.

2. **`sa-mooc-worker`**:
   - `roles/cloudsql.client`: Actualizar el estado de los recursos a `completed`.
   - `roles/storage.objectAdmin`: Descargar videos originales y subir segmentos HLS y playlists.
   - `roles/secretmanager.secretAccessor`: Leer secretos de conexión y almacenamiento.

3. **`sa-pubsub-invoker`**:
   - `roles/run.invoker`: Autorización exclusiva para invocar el endpoint privado del worker en Cloud Run (`--no-allow-unauthenticated`).

### Comandos de Creación y Asignación de Roles:
```bash
# Creación de Service Accounts
gcloud iam service-accounts create sa-mooc-backend --display-name="Backend Cloud Run SA"
gcloud iam service-accounts create sa-mooc-worker --display-name="Worker Cloud Run SA"
gcloud iam service-accounts create sa-pubsub-invoker --display-name="PubSub Push Invoker SA"

# Asignación Backend
gcloud projects add-iam-policy-binding $PROJECT_ID \
  --member="serviceAccount:sa-mooc-backend@$PROJECT_ID.iam.gserviceaccount.com" \
  --role="roles/cloudsql.client"
gcloud projects add-iam-policy-binding $PROJECT_ID \
  --member="serviceAccount:sa-mooc-backend@$PROJECT_ID.iam.gserviceaccount.com" \
  --role="roles/pubsub.publisher"
gcloud projects add-iam-policy-binding $PROJECT_ID \
  --member="serviceAccount:sa-mooc-backend@$PROJECT_ID.iam.gserviceaccount.com" \
  --role="roles/storage.objectAdmin"
gcloud projects add-iam-policy-binding $PROJECT_ID \
  --member="serviceAccount:sa-mooc-backend@$PROJECT_ID.iam.gserviceaccount.com" \
  --role="roles/secretmanager.secretAccessor"

# Asignación Worker
gcloud projects add-iam-policy-binding $PROJECT_ID \
  --member="serviceAccount:sa-mooc-worker@$PROJECT_ID.iam.gserviceaccount.com" \
  --role="roles/cloudsql.client"
gcloud projects add-iam-policy-binding $PROJECT_ID \
  --member="serviceAccount:sa-mooc-worker@$PROJECT_ID.iam.gserviceaccount.com" \
  --role="roles/storage.objectAdmin"
gcloud projects add-iam-policy-binding $PROJECT_ID \
  --member="serviceAccount:sa-mooc-worker@$PROJECT_ID.iam.gserviceaccount.com" \
  --role="roles/secretmanager.secretAccessor"
```

---

## 5. Gestión de Secretos (Secret Manager)

Asegurar que los secretos existan sin exponer contraseñas en Git:
```bash
# 1. URL de la Base de Datos PostgreSQL (usando socket de Cloud SQL)
echo -n "postgres://mooc_user:TU_DB_PASSWORD@/mooc_db?host=/cloudsql/$PROJECT_ID:$REGION:$SQL_INSTANCE&sslmode=disable" | \
  gcloud secrets create mooc-db-url --data-file=- 2>/dev/null || \
  echo -n "postgres://mooc_user:TU_DB_PASSWORD@/mooc_db?host=/cloudsql/$PROJECT_ID:$REGION:$SQL_INSTANCE&sslmode=disable" | \
  gcloud secrets versions add mooc-db-url --data-file=-

# 2. JWT Secret
echo -n "clave-secreta-jwt-de-produccion" | \
  gcloud secrets create mooc-jwt-secret --data-file=- 2>/dev/null || \
  echo -n "clave-secreta-jwt-de-produccion" | \
  gcloud secrets versions add mooc-jwt-secret --data-file=-
```

---

## 6. Construcción y Publicación de Imágenes

```bash
# Backend (API)
docker build -t $REPO/mooc-backend:latest -f backend/Dockerfile.backend backend/
docker push $REPO/mooc-backend:latest

# Worker (FFmpeg Transcoder)
docker build -t $REPO/mooc-worker:latest -f backend/Dockerfile.worker backend/
docker push $REPO/mooc-worker:latest
```

---

## 7. Despliegue del Servicio Worker (Cloud Run)

El Worker se despliega de forma **privada** con autoescalado de máximo 3 instancias, timeout de 600 segundos (para soportar videos largos) y concurrencia baja (1 tarea pesada por contenedor):

```bash
gcloud run deploy mooc-worker \
  --image=$REPO/mooc-worker:latest \
  --region=$REGION \
  --service-account="sa-mooc-worker@$PROJECT_ID.iam.gserviceaccount.com" \
  --no-allow-unauthenticated \
  --max-instances=3 \
  --min-instances=0 \
  --concurrency=1 \
  --cpu=2 \
  --memory=2Gi \
  --timeout=600s \
  --add-cloudsql-instances="$PROJECT_ID:$REGION:$SQL_INSTANCE" \
  --set-secrets="DATABASE_URL=mooc-db-url:latest,JWT_SECRET=mooc-jwt-secret:latest" \
  --set-env-vars="S3_REGION=$REGION,S3_MEDIA_BUCKET=$MEDIA_BUCKET,MINIO_ENDPOINT=storage.googleapis.com,MINIO_SECURE=true"

# Obtener URL del Worker
export WORKER_URL=$(gcloud run services describe mooc-worker --region=$REGION --format="value(status.url)")
echo "Worker URL: $WORKER_URL"
```

Autorizar a la cuenta de servicio `sa-pubsub-invoker` para invocar al worker privado:
```bash
gcloud run services add-iam-policy-binding mooc-worker \
  --region=$REGION \
  --member="serviceAccount:sa-pubsub-invoker@$PROJECT_ID.iam.gserviceaccount.com" \
  --role="roles/run.invoker"
```

---

## 8. Configuración de Cloud Pub/Sub (Topic y Suscripción Push)

```bash
# 1. Crear el topic
gcloud pubsub topics create $TOPIC_ID 2>/dev/null || true

# 2. Crear suscripción Push con autenticación OIDC y ack-deadline de 600s
gcloud pubsub subscriptions create $SUB_ID \
  --topic=$TOPIC_ID \
  --push-endpoint="$WORKER_URL/pubsub/transcode" \
  --push-auth-service-account="sa-pubsub-invoker@$PROJECT_ID.iam.gserviceaccount.com" \
  --ack-deadline=600 \
  --min-retry-delay=10s \
  --max-retry-delay=300s
```

---

## 9. Despliegue del Servicio Backend (Cloud Run)

```bash
gcloud run deploy mooc-backend \
  --image=$REPO/mooc-backend:latest \
  --region=$REGION \
  --service-account="sa-mooc-backend@$PROJECT_ID.iam.gserviceaccount.com" \
  --allow-unauthenticated \
  --max-instances=3 \
  --min-instances=0 \
  --concurrency=80 \
  --cpu=1 \
  --memory=1Gi \
  --timeout=60s \
  --add-cloudsql-instances="$PROJECT_ID:$REGION:$SQL_INSTANCE" \
  --set-secrets="DATABASE_URL=mooc-db-url:latest,JWT_SECRET=mooc-jwt-secret:latest" \
  --set-env-vars="GCP_PROJECT=$PROJECT_ID,PUBSUB_TOPIC=$TOPIC_ID,S3_REGION=$REGION,S3_MEDIA_BUCKET=$MEDIA_BUCKET,MINIO_ENDPOINT=storage.googleapis.com,MINIO_SECURE=true"

# Obtener URL del Backend
export BACKEND_URL=$(gcloud run services describe mooc-backend --region=$REGION --format="value(status.url)")
echo "Backend URL: $BACKEND_URL"
```

---

## 10. Construcción y Despliegue del Servicio Frontend (Cloud Run)

1. Compilar la imagen inyectando la URL pública del backend:
   ```bash
   docker build \
     --build-arg VITE_API_URL=$BACKEND_URL \
     -t $REPO/mooc-frontend:latest \
     -f frontend/Dockerfile frontend/
   docker push $REPO/mooc-frontend:latest
   ```

2. Desplegar el Frontend en Cloud Run con máx. 3 instancias:
   ```bash
   gcloud run deploy mooc-frontend \
     --image=$REPO/mooc-frontend:latest \
     --region=$REGION \
     --allow-unauthenticated \
     --max-instances=3 \
     --min-instances=0 \
     --concurrency=80 \
     --cpu=1 \
     --memory=512Mi \
     --timeout=60s

   export FRONTEND_URL=$(gcloud run services describe mooc-frontend --region=$REGION --format="value(status.url)")
   echo "Frontend URL: $FRONTEND_URL"
   ```

3. Actualizar la política CORS en el Backend para autorizar el origen del Frontend:
   ```bash
   gcloud run services update mooc-backend \
     --region=$REGION \
     --update-env-vars="CORS_ALLOWED_ORIGINS=$FRONTEND_URL"
   ```

---

## 11. Ejecución Automática mediante Script

Todo el proceso anterior se encuentra completamente automatizado en un único script de PowerShell incluido en el repositorio:

```powershell
.\scripts\gcp\desplegar-entrega4.ps1
```

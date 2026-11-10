# Script de Despliegue Automatizado para la Entrega 4: Cloud Run y Cloud Pub/Sub
# Requiere Google Cloud CLI (gcloud) autenticado con permisos en el proyecto.
param(
    [string]$Project      = "proyecto-plataforma-mooc",
    [string]$Region       = "us-central1",
    [string]$RepoName     = "mooc-repo",
    [string]$SqlInstance  = "mooc-db-instance",
    [string]$MediaBucket  = "mooc-media-equipo-3",
    [string]$TopicName    = "mooc-tasks-topic",
    [string]$SubName      = "mooc-worker-push-sub",
    [switch]$SkipBuild
)

$ErrorActionPreference = "Stop"

function Write-Step([string]$msg) {
    Write-Host "`n========================================================" -ForegroundColor Cyan
    Write-Host ">>> $msg" -ForegroundColor Cyan
    Write-Host "========================================================" -ForegroundColor Cyan
}

function Write-Success([string]$msg) {
    Write-Host "[OK] $msg" -ForegroundColor Green
}

function Write-Info([string]$msg) {
    Write-Host "[INFO] $msg" -ForegroundColor Yellow
}

# 1. Configuración de proyecto
Write-Step "1. Configurando proyecto GCP: $Project en región $Region"
gcloud config set project $Project
$ProjectNumber = (gcloud projects describe $Project --format="value(projectNumber)").Trim()
Write-Success "Proyecto configurado. Número de proyecto: $ProjectNumber"

# 2. Habilitar APIs necesarias
Write-Step "2. Habilitando APIs requeridas"
gcloud services enable `
    run.googleapis.com `
    pubsub.googleapis.com `
    artifactregistry.googleapis.com `
    secretmanager.googleapis.com `
    cloudbuild.googleapis.com `
    sqladmin.googleapis.com
Write-Success "APIs habilitadas."

# 3. Artifact Registry
Write-Step "3. Verificando repositorio en Artifact Registry"
$repoExists = gcloud artifacts repositories list --location=$Region --filter="name:$RepoName" --format="value(name)"
if (-not $repoExists) {
    Write-Info "Creando repositorio $RepoName en $Region..."
    gcloud artifacts repositories create $RepoName `
        --repository-format=docker `
        --location=$Region `
        --description="Repositorio de contenedores Entrega 4"
}
$RepoUrl = "$Region-docker.pkg.dev/$Project/$RepoName"
Write-Success "Artifact Registry listo: $RepoUrl"

# 4. Cuentas de Servicio con Mínimos Privilegios
Write-Step "4. Configurando Cuentas de Servicio e IAM"
$BackendSA = "sa-mooc-backend"
$WorkerSA  = "sa-mooc-worker"
$InvokerSA = "sa-pubsub-invoker"

$BackendEmail = "$BackendSA@$Project.iam.gserviceaccount.com"
$WorkerEmail  = "$WorkerSA@$Project.iam.gserviceaccount.com"
$InvokerEmail = "$InvokerSA@$Project.iam.gserviceaccount.com"

foreach ($sa in @($BackendSA, $WorkerSA, $InvokerSA)) {
    $exists = gcloud iam service-accounts list --filter="name:$sa" --format="value(email)"
    if (-not $exists) {
        Write-Info "Creando cuenta de servicio: $sa"
        gcloud iam service-accounts create $sa --display-name="Cuenta de servicio $sa"
    }
}

# Asignar roles mínimos para Backend
gcloud projects add-iam-policy-binding $Project --member="serviceAccount:$BackendEmail" --role="roles/cloudsql.client" --condition=None --quiet | Out-Null
gcloud projects add-iam-policy-binding $Project --member="serviceAccount:$BackendEmail" --role="roles/pubsub.publisher" --condition=None --quiet | Out-Null
gcloud projects add-iam-policy-binding $Project --member="serviceAccount:$BackendEmail" --role="roles/storage.objectAdmin" --condition=None --quiet | Out-Null
gcloud projects add-iam-policy-binding $Project --member="serviceAccount:$BackendEmail" --role="roles/secretmanager.secretAccessor" --condition=None --quiet | Out-Null

# Asignar roles mínimos para Worker
gcloud projects add-iam-policy-binding $Project --member="serviceAccount:$WorkerEmail" --role="roles/cloudsql.client" --condition=None --quiet | Out-Null
gcloud projects add-iam-policy-binding $Project --member="serviceAccount:$WorkerEmail" --role="roles/storage.objectAdmin" --condition=None --quiet | Out-Null
gcloud projects add-iam-policy-binding $Project --member="serviceAccount:$WorkerEmail" --role="roles/secretmanager.secretAccessor" --condition=None --quiet | Out-Null

Write-Success "Cuentas de servicio y permisos IAM configurados."

# 5. Cloud Pub/Sub Topic
Write-Step "5. Configurando Topic de Cloud Pub/Sub: $TopicName"
$topicExists = gcloud pubsub topics list --filter="name:$TopicName" --format="value(name)"
if (-not $topicExists) {
    gcloud pubsub topics create $TopicName
}
Write-Success "Topic listo: $TopicName"

# 6. Construcción y Publicación de Imágenes
$BackendImage  = "$RepoUrl/mooc-backend:latest"
$WorkerImage   = "$RepoUrl/mooc-worker:latest"
$FrontendImage = "$RepoUrl/mooc-frontend:latest"

if (-not $SkipBuild) {
    Write-Step "6. Construyendo y publicando imágenes de contenedor con Google Cloud Build"
    Write-Info "Enviando construcción de Backend y Worker a Cloud Build..."
    gcloud builds submit --config=cloudbuild.yaml --substitutions=_REPO_URL="$RepoUrl" .
    Write-Success "Imágenes de Backend y Worker publicadas exitosamente en Artifact Registry."
}

# 7. Desplegar Worker en Cloud Run (Invocación Privada)
Write-Step "7. Desplegando Servicio Cloud Run: mooc-worker"
gcloud run deploy mooc-worker `
    --image=$WorkerImage `
    --region=$Region `
    --service-account=$WorkerEmail `
    --no-allow-unauthenticated `
    --max-instances=3 `
    --min-instances=0 `
    --concurrency=1 `
    --cpu=2 `
    --memory=2Gi `
    --timeout=600s `
    --add-cloudsql-instances="$Project:$Region:$SqlInstance" `
    --set-secrets="DATABASE_URL=mooc-db-url:latest,JWT_SECRET=mooc-jwt-secret:latest" `
    --set-env-vars="S3_REGION=$Region,S3_MEDIA_BUCKET=$MediaBucket,MINIO_ENDPOINT=storage.googleapis.com,MINIO_SECURE=true" `
    --quiet

$WorkerUrl = (gcloud run services describe mooc-worker --region=$Region --format="value(status.url)").Trim()
Write-Success "Worker desplegado en: $WorkerUrl"

# 8. Autorizar a sa-pubsub-invoker a invocar el Worker
Write-Step "8. Autorizando a la suscripción push a invocar mooc-worker"
gcloud run services add-iam-policy-binding mooc-worker `
    --region=$Region `
    --member="serviceAccount:$InvokerEmail" `
    --role="roles/run.invoker" `
    --quiet | Out-Null
Write-Success "Invocador autorizado."

# 9. Crear/Actualizar Suscripción Push de Pub/Sub
Write-Step "9. Configurando Suscripción Push: $SubName"
$subExists = gcloud pubsub subscriptions list --filter="name:$SubName" --format="value(name)"
$PushEndpoint = "$WorkerUrl/pubsub/transcode"

if ($subExists) {
    Write-Info "Actualizando endpoint de la suscripción push existente..."
    gcloud pubsub subscriptions modify-push-config $SubName `
        --push-endpoint=$PushEndpoint `
        --push-auth-service-account=$InvokerEmail
} else {
    Write-Info "Creando suscripción push autenticada..."
    gcloud pubsub subscriptions create $SubName `
        --topic=$TopicName `
        --push-endpoint=$PushEndpoint `
        --push-auth-service-account=$InvokerEmail `
        --ack-deadline=600 `
        --min-retry-delay=10s `
        --max-retry-delay=300s
}
Write-Success "Suscripción Push configurada apuntando a: $PushEndpoint"

# 10. Desplegar Backend en Cloud Run
Write-Step "10. Desplegando Servicio Cloud Run: mooc-backend"
gcloud run deploy mooc-backend `
    --image=$BackendImage `
    --region=$Region `
    --service-account=$BackendEmail `
    --allow-unauthenticated `
    --max-instances=3 `
    --min-instances=0 `
    --concurrency=80 `
    --cpu=1 `
    --memory=1Gi `
    --timeout=60s `
    --add-cloudsql-instances="$Project:$Region:$SqlInstance" `
    --set-secrets="DATABASE_URL=mooc-db-url:latest,JWT_SECRET=mooc-jwt-secret:latest" `
    --set-env-vars="GCP_PROJECT=$Project,PUBSUB_TOPIC=$TopicName,S3_REGION=$Region,S3_MEDIA_BUCKET=$MediaBucket,MINIO_ENDPOINT=storage.googleapis.com,MINIO_SECURE=true" `
    --quiet

$BackendUrl = (gcloud run services describe mooc-backend --region=$Region --format="value(status.url)").Trim()
Write-Success "Backend desplegado en: $BackendUrl"

# 11. Compilar y Desplegar Frontend
Write-Step "11. Compilando y desplegando Servicio Cloud Run: mooc-frontend"
if (-not $SkipBuild) {
    Write-Info "Construyendo Frontend con Cloud Build inyectando VITE_API_URL=$BackendUrl..."
    gcloud builds submit --config=cloudbuild-frontend.yaml --substitutions=_REPO_URL="$RepoUrl",_BACKEND_URL="$BackendUrl" .
}

gcloud run deploy mooc-frontend `
    --image=$FrontendImage `
    --region=$Region `
    --allow-unauthenticated `
    --max-instances=3 `
    --min-instances=0 `
    --concurrency=80 `
    --cpu=1 `
    --memory=512Mi `
    --timeout=60s `
    --quiet

$FrontendUrl = (gcloud run services describe mooc-frontend --region=$Region --format="value(status.url)").Trim()
Write-Success "Frontend desplegado en: $FrontendUrl"

# 12. Configurar CORS en el Backend apuntando al Frontend
Write-Step "12. Configurando CORS en Backend para permitir el Frontend"
gcloud run services update mooc-backend `
    --region=$Region `
    --update-env-vars="CORS_ALLOWED_ORIGINS=$FrontendUrl" `
    --quiet
Write-Success "CORS configurado para origen: $FrontendUrl"

# Resumen Final
Write-Host "`n========================================================" -ForegroundColor Green
Write-Host "DESPLIEGUE DE LA ENTREGA 4 COMPLETADO EXITOSAMENTE" -ForegroundColor Green
Write-Host "========================================================" -ForegroundColor Green
Write-Host "Frontend URL : $FrontendUrl" -ForegroundColor Cyan
Write-Host "Backend URL  : $BackendUrl" -ForegroundColor Cyan
Write-Host "Worker URL   : $WorkerUrl (Privado, máx 3 instancias)" -ForegroundColor Cyan
Write-Host "Topic PubSub : $TopicName" -ForegroundColor Cyan
Write-Host "Push Sub     : $SubName" -ForegroundColor Cyan
Write-Host "`nPara apagar las VMs anteriores y ahorrar costos:" -ForegroundColor Yellow
Write-Host "  .\\scripts\\gcp\\apagar.ps1" -ForegroundColor Yellow

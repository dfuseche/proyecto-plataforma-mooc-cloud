# Cuentas de servicio con mínimos privilegios. Los roles se asignan sobre el recurso concreto
# (bucket, topic, secreto, servicio) siempre que el rol lo permite.

locals {
  sfx = var.name_suffix
}

resource "google_service_account" "backend" {
  account_id   = "sa-mooc-backend${local.sfx}"
  display_name = "Backend (Cloud Run)"
  depends_on   = [google_project_service.apis]
}

resource "google_service_account" "worker" {
  account_id   = "sa-mooc-worker${local.sfx}"
  display_name = "Worker de transcodificación (Cloud Run)"
  depends_on   = [google_project_service.apis]
}

resource "google_service_account" "invoker" {
  account_id   = "sa-pubsub-invoker${local.sfx}"
  display_name = "Pub/Sub push que invoca al worker"
  depends_on   = [google_project_service.apis]
}

resource "google_service_account" "scan" {
  account_id   = "sa-mooc-scan${local.sfx}"
  display_name = "Cloud Function de escaneo antimalware"
  depends_on   = [google_project_service.apis]
}

resource "google_service_account" "trigger" {
  account_id   = "sa-eventarc-scan${local.sfx}"
  display_name = "Disparador Eventarc de la función de escaneo"
  depends_on   = [google_project_service.apis]
}

# --- Cloud SQL (a nivel de proyecto: es el único alcance del rol) ---
resource "google_project_iam_member" "backend_sql" {
  project = var.project_id
  role    = "roles/cloudsql.client"
  member  = "serviceAccount:${google_service_account.backend.email}"
}

resource "google_project_iam_member" "worker_sql" {
  project = var.project_id
  role    = "roles/cloudsql.client"
  member  = "serviceAccount:${google_service_account.worker.email}"
}

# --- Cloud Storage: solo sobre el bucket de medios ---
data "google_storage_bucket" "media" {
  name = var.media_bucket
}

resource "google_storage_bucket_iam_member" "backend_media" {
  bucket = data.google_storage_bucket.media.name
  role   = "roles/storage.objectAdmin"
  member = "serviceAccount:${google_service_account.backend.email}"
}

resource "google_storage_bucket_iam_member" "worker_media" {
  bucket = data.google_storage_bucket.media.name
  role   = "roles/storage.objectAdmin"
  member = "serviceAccount:${google_service_account.worker.email}"
}

# La función lee la cabecera del objeto y actualiza su metadata: objectUser alcanza.
resource "google_storage_bucket_iam_member" "scan_media" {
  bucket = data.google_storage_bucket.media.name
  role   = "roles/storage.objectUser"
  member = "serviceAccount:${google_service_account.scan.email}"
}

# --- Secret Manager: solo los secretos que cada servicio usa ---
locals {
  secret_names = {
    db_url         = var.secret_db_url
    jwt            = var.secret_jwt
    storage_access = var.secret_storage_access_key
    storage_secret = var.secret_storage_secret_key
  }
}

# Se referencian los secretos existentes; Terraform nunca lee ni guarda su valor.
data "google_secret_manager_secret" "app" {
  for_each  = local.secret_names
  secret_id = each.value
}

resource "google_secret_manager_secret_iam_member" "backend" {
  for_each  = data.google_secret_manager_secret.app
  secret_id = each.value.id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${google_service_account.backend.email}"
}

resource "google_secret_manager_secret_iam_member" "worker" {
  for_each  = data.google_secret_manager_secret.app
  secret_id = each.value.id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${google_service_account.worker.email}"
}

# --- Disparador de la función ---
resource "google_project_iam_member" "trigger_receiver" {
  project = var.project_id
  role    = "roles/eventarc.eventReceiver"
  member  = "serviceAccount:${google_service_account.trigger.email}"
}

# Cloud Build compila la función con la cuenta de Compute por omisión; en proyectos nuevos
# no trae permisos para hacerlo.
resource "google_project_iam_member" "build_builder" {
  project = var.project_id
  role    = "roles/cloudbuild.builds.builder"
  member  = "serviceAccount:${data.google_project.this.number}-compute@developer.gserviceaccount.com"

  depends_on = [google_project_service.apis]
}

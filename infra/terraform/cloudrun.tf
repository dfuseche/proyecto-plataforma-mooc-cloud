locals {
  image_base = "${var.region}-docker.pkg.dev/${var.project_id}/${var.artifact_repo}"

  backend_name  = "mooc-backend${local.sfx}"
  worker_name   = "mooc-worker${local.sfx}"
  frontend_name = "mooc-frontend${local.sfx}"

  # Las URLs de Cloud Run son determinísticas, así que el frontend se puede construir con la
  # URL del backend, y el backend recibir el origen del frontend, sin dependencia circular.
  backend_url  = "https://${local.backend_name}-${data.google_project.this.number}.${var.region}.run.app"
  frontend_url = "https://${local.frontend_name}-${data.google_project.this.number}.${var.region}.run.app"

  sql_connection = "${var.project_id}:${var.region}:${var.sql_instance}"

  # Variable de entorno -> clave de local.secret_names
  app_secrets = {
    DATABASE_URL     = "db_url"
    JWT_SECRET       = "jwt"
    MINIO_ACCESS_KEY = "storage_access"
    MINIO_SECRET_KEY = "storage_secret"
  }

  storage_env = {
    S3_REGION       = var.region
    S3_MEDIA_BUCKET = var.media_bucket
    MINIO_ENDPOINT  = "storage.googleapis.com"
    MINIO_SECURE    = "true"
  }
}

# ---------------- Backend (API pública) ----------------
resource "google_cloud_run_v2_service" "backend" {
  name                = local.backend_name
  location            = var.region
  ingress             = "INGRESS_TRAFFIC_ALL"
  deletion_protection = false

  template {
    service_account                  = google_service_account.backend.email
    timeout                          = "60s"
    max_instance_request_concurrency = 80

    scaling {
      min_instance_count = 0
      max_instance_count = var.max_instances
    }

    volumes {
      name = "cloudsql"
      cloud_sql_instance {
        instances = [local.sql_connection]
      }
    }

    containers {
      image = "${local.image_base}/mooc-backend:${var.image_tag}"

      resources {
        limits = {
          cpu    = "1"
          memory = "1Gi"
        }
      }

      volume_mounts {
        name       = "cloudsql"
        mount_path = "/cloudsql"
      }

      env {
        name  = "GCP_PROJECT"
        value = var.project_id
      }
      env {
        name  = "PUBSUB_TOPIC"
        value = google_pubsub_topic.tasks.name
      }
      env {
        name  = "PUBSUB_SCAN_TOPIC"
        value = google_pubsub_topic.scan.name
      }
      env {
        name  = "CORS_ALLOWED_ORIGINS"
        value = local.frontend_url
      }
      dynamic "env" {
        for_each = local.storage_env
        content {
          name  = env.key
          value = env.value
        }
      }
      dynamic "env" {
        for_each = local.app_secrets
        content {
          name = env.key
          value_source {
            secret_key_ref {
              secret  = local.secret_names[env.value]
              version = "latest"
            }
          }
        }
      }
    }
  }

  depends_on = [
    google_artifact_registry_repository.images,
    google_secret_manager_secret_iam_member.backend,
    google_project_iam_member.backend_sql,
    google_storage_bucket_iam_member.backend_media,
  ]
}

resource "google_cloud_run_v2_service_iam_member" "backend_public" {
  name     = google_cloud_run_v2_service.backend.name
  location = var.region
  role     = "roles/run.invoker"
  member   = "allUsers"
}

# ---------------- Worker (privado, lo invoca Pub/Sub) ----------------
resource "google_cloud_run_v2_service" "worker" {
  name                = local.worker_name
  location            = var.region
  ingress             = "INGRESS_TRAFFIC_ALL"
  deletion_protection = false

  template {
    service_account                  = google_service_account.worker.email
    timeout                          = "600s"
    max_instance_request_concurrency = 1

    scaling {
      min_instance_count = 0
      max_instance_count = var.max_instances
    }

    volumes {
      name = "cloudsql"
      cloud_sql_instance {
        instances = [local.sql_connection]
      }
    }

    containers {
      image = "${local.image_base}/mooc-worker:${var.image_tag}"

      resources {
        limits = {
          cpu    = "2"
          memory = "2Gi"
        }
      }

      volume_mounts {
        name       = "cloudsql"
        mount_path = "/cloudsql"
      }

      dynamic "env" {
        for_each = local.storage_env
        content {
          name  = env.key
          value = env.value
        }
      }
      dynamic "env" {
        for_each = local.app_secrets
        content {
          name = env.key
          value_source {
            secret_key_ref {
              secret  = local.secret_names[env.value]
              version = "latest"
            }
          }
        }
      }
    }
  }

  depends_on = [
    google_artifact_registry_repository.images,
    google_secret_manager_secret_iam_member.worker,
    google_project_iam_member.worker_sql,
    google_storage_bucket_iam_member.worker_media,
  ]
}

# Solo la cuenta del push de Pub/Sub puede invocar al worker.
resource "google_cloud_run_v2_service_iam_member" "worker_invoker" {
  name     = google_cloud_run_v2_service.worker.name
  location = var.region
  role     = "roles/run.invoker"
  member   = "serviceAccount:${google_service_account.invoker.email}"
}

# ---------------- Frontend (SPA en Nginx, pública) ----------------
resource "google_cloud_run_v2_service" "frontend" {
  name                = local.frontend_name
  location            = var.region
  ingress             = "INGRESS_TRAFFIC_ALL"
  deletion_protection = false

  template {
    timeout                          = "60s"
    max_instance_request_concurrency = 80

    scaling {
      min_instance_count = 0
      max_instance_count = var.max_instances
    }

    containers {
      image = "${local.image_base}/mooc-frontend:${var.image_tag}"

      resources {
        limits = {
          cpu    = "1"
          memory = "512Mi"
        }
      }
    }
  }

  depends_on = [google_artifact_registry_repository.images]
}

resource "google_cloud_run_v2_service_iam_member" "frontend_public" {
  name     = google_cloud_run_v2_service.frontend.name
  location = var.region
  role     = "roles/run.invoker"
  member   = "allUsers"
}

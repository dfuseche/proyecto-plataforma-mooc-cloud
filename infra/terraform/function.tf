# Cloud Function de 2.ª generación (Cloud Run functions) que escanea cada objeto subido.
# Reemplaza a la tarea media:antimalware_scan del worker. Código en functions/antimalware-scan.

data "archive_file" "scan_source" {
  type        = "zip"
  source_dir  = "${path.module}/../../functions/antimalware-scan"
  output_path = "${path.module}/.build/antimalware-scan.zip"
  excludes    = ["README.md", "scanner/scanner_test.go"]
}

# Bucket donde se sube el código empaquetado (distinto del bucket de medios).
resource "google_storage_bucket" "function_source" {
  name                        = "${var.project_id}-fn-source${local.sfx}"
  location                    = var.region
  uniform_bucket_level_access = true
  force_destroy               = true

  depends_on = [google_project_service.apis]
}

# El nombre incluye el hash del zip: al cambiar el código se sube un objeto nuevo y la función se actualiza.
resource "google_storage_bucket_object" "scan_source" {
  name   = "antimalware-scan-${data.archive_file.scan_source.output_md5}.zip"
  bucket = google_storage_bucket.function_source.name
  source = data.archive_file.scan_source.output_path
}

resource "google_cloudfunctions2_function" "scan" {
  name     = "mooc-antimalware-scan${local.sfx}"
  location = var.region

  build_config {
    runtime     = var.scan_function_runtime
    entry_point = "ScanUpload"

    source {
      storage_source {
        bucket = google_storage_bucket.function_source.name
        object = google_storage_bucket_object.scan_source.name
      }
    }
  }

  service_config {
    max_instance_count    = var.max_instances
    min_instance_count    = 0
    available_memory      = "256M"
    timeout_seconds       = 60
    service_account_email = google_service_account.scan.email

    environment_variables = {
      MEDIA_BUCKET     = var.media_bucket
      MAX_OBJECT_BYTES = tostring(var.scan_max_object_bytes)
    }
  }

  event_trigger {
    trigger_region        = var.region
    event_type            = "google.cloud.pubsub.topic.v1.messagePublished"
    pubsub_topic          = google_pubsub_topic.scan.id
    retry_policy          = "RETRY_POLICY_RETRY"
    service_account_email = google_service_account.trigger.email
  }

  depends_on = [
    google_project_service.apis,
    google_project_iam_member.build_builder,
    google_project_iam_member.trigger_receiver,
    google_storage_bucket_iam_member.scan_media,
  ]
}

# Eventarc entrega los eventos invocando el servicio de Cloud Run que respalda la función.
resource "google_cloud_run_v2_service_iam_member" "scan_invoker" {
  name     = google_cloudfunctions2_function.scan.service_config[0].service
  location = var.region
  role     = "roles/run.invoker"
  member   = "serviceAccount:${google_service_account.trigger.email}"
}

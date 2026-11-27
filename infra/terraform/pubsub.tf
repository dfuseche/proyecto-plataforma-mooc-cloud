# Topic de transcodificación: el backend publica y Pub/Sub entrega por push (OIDC) al worker.
resource "google_pubsub_topic" "tasks" {
  name       = "mooc-tasks-topic${local.sfx}"
  depends_on = [google_project_service.apis]
}

# Topic del escaneo antimalware: el backend publica y Eventarc dispara la Cloud Function.
resource "google_pubsub_topic" "scan" {
  name       = "mooc-scan-topic${local.sfx}"
  depends_on = [google_project_service.apis]
}

resource "google_pubsub_topic_iam_member" "backend_tasks" {
  topic  = google_pubsub_topic.tasks.name
  role   = "roles/pubsub.publisher"
  member = "serviceAccount:${google_service_account.backend.email}"
}

resource "google_pubsub_topic_iam_member" "backend_scan" {
  topic  = google_pubsub_topic.scan.name
  role   = "roles/pubsub.publisher"
  member = "serviceAccount:${google_service_account.backend.email}"
}

resource "google_pubsub_subscription" "worker_push" {
  name                 = "mooc-worker-push-sub${local.sfx}"
  topic                = google_pubsub_topic.tasks.id
  ack_deadline_seconds = 600

  push_config {
    push_endpoint = "${google_cloud_run_v2_service.worker.uri}/pubsub/transcode"

    oidc_token {
      service_account_email = google_service_account.invoker.email
      audience              = google_cloud_run_v2_service.worker.uri
    }
  }

  retry_policy {
    minimum_backoff = "10s"
    maximum_backoff = "300s"
  }
}

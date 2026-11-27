output "frontend_url" {
  description = "URL pública del frontend (la que el backend permite por CORS)."
  value       = local.frontend_url
}

output "backend_url" {
  description = "URL pública de la API (la misma con la que se construye el frontend)."
  value       = local.backend_url
}

output "worker_url" {
  description = "URL del worker (privado: solo lo invoca la suscripción push)."
  value       = google_cloud_run_v2_service.worker.uri
}

output "tasks_topic" {
  value = google_pubsub_topic.tasks.name
}

output "scan_topic" {
  value = google_pubsub_topic.scan.name
}

output "scan_function" {
  value = google_cloudfunctions2_function.scan.name
}

output "backend_url_para_el_frontend" {
  description = "Valor de VITE_API_URL con el que se construye la imagen del frontend (se conoce antes del apply)."
  value       = local.backend_url
}

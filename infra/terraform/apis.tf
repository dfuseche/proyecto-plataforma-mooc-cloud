data "google_project" "this" {
  project_id = var.project_id
}

locals {
  apis = [
    "run.googleapis.com",
    "pubsub.googleapis.com",
    "artifactregistry.googleapis.com",
    "secretmanager.googleapis.com",
    "sqladmin.googleapis.com",
    "cloudfunctions.googleapis.com",
    "eventarc.googleapis.com",
    "cloudbuild.googleapis.com",
    "iam.googleapis.com",
    "storage.googleapis.com",
  ]
}

resource "google_project_service" "apis" {
  for_each           = toset(local.apis)
  service            = each.value
  disable_on_destroy = false
}

# Repositorio de imágenes. Si ya existe de un despliegue anterior, impórtalo en vez de crearlo:
#   terraform import google_artifact_registry_repository.images projects/<proyecto>/locations/<región>/repositories/<repo>
resource "google_artifact_registry_repository" "images" {
  location      = var.region
  repository_id = var.artifact_repo
  description   = "Imágenes de la plataforma MOOC"
  format        = "DOCKER"

  depends_on = [google_project_service.apis]
}

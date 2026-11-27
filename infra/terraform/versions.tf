terraform {
  required_version = ">= 1.5.0"

  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "~> 6.0"
    }
    archive = {
      source  = "hashicorp/archive"
      version = "~> 2.4"
    }
  }

  # El estado vive en un bucket de Cloud Storage que NO está en el repositorio ni lo
  # gestiona este código. El bucket se pasa al inicializar:
  #   terraform init -backend-config="bucket=<bucket-de-estado>"
  backend "gcs" {
    prefix = "mooc/entrega5"
  }
}

provider "google" {
  project = var.project_id
  region  = var.region
}

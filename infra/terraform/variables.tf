variable "project_id" {
  description = "ID del proyecto de GCP."
  type        = string
  default     = "proyecto-plataforma-mooc"
}

variable "region" {
  description = "Región de Cloud Run, Pub/Sub, Artifact Registry y la función."
  type        = string
  default     = "us-central1"
}

variable "name_suffix" {
  description = <<-EOT
    Sufijo que se agrega al nombre de todo lo que crea Terraform (servicios, topics, cuentas de
    servicio). Con "-tf" convive con el despliegue por script de la Entrega 4 en el mismo proyecto;
    con "" se usan los nombres originales (requiere borrar antes los recursos del script).
  EOT
  type        = string
  default     = "-tf"
}

# --- Recursos existentes que Terraform solo referencia (no los crea ni los destruye) ---

variable "media_bucket" {
  description = "Bucket de Cloud Storage con los videos y materiales (ya existe)."
  type        = string
  default     = "mooc-media-equipo-3"
}

variable "sql_instance" {
  description = "Instancia de Cloud SQL PostgreSQL (ya existe)."
  type        = string
  default     = "mooc-db-instance"
}

variable "secret_db_url" {
  description = "Nombre del secreto con DATABASE_URL (ya existe; su valor no está en Terraform)."
  type        = string
  default     = "mooc-db-url"
}

variable "secret_jwt" {
  description = "Nombre del secreto con JWT_SECRET."
  type        = string
  default     = "mooc-jwt-secret"
}

variable "secret_storage_access_key" {
  description = "Nombre del secreto con la clave HMAC de acceso a Cloud Storage."
  type        = string
  default     = "mooc-storage-access-key"
}

variable "secret_storage_secret_key" {
  description = "Nombre del secreto con la clave HMAC secreta de Cloud Storage."
  type        = string
  default     = "mooc-storage-secret-key"
}

# --- Imágenes ---

variable "artifact_repo" {
  description = "Repositorio de Artifact Registry donde están las imágenes (se crea si no existe en el proyecto)."
  type        = string
  default     = "mooc-repo"
}

variable "image_tag" {
  description = "Tag de las imágenes mooc-backend, mooc-worker y mooc-frontend (por ejemplo, el hash del commit)."
  type        = string
  default     = "latest"
}

# --- Escalamiento ---

variable "max_instances" {
  description = "Máximo de instancias por servicio de Cloud Run y por la función."
  type        = number
  default     = 3
}

# --- Cloud Function de escaneo antimalware ---

variable "scan_function_runtime" {
  description = "Runtime de Go de la Cloud Function."
  type        = string
  default     = "go127"
}

variable "scan_max_object_bytes" {
  description = "Tamaño máximo de objeto que la función acepta como válido (bytes)."
  type        = number
  default     = 5368709120
}

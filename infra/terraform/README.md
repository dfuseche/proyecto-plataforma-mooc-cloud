# Despliegue con Terraform (Entrega 5)

Terraform gestiona: los 3 servicios de Cloud Run (imagen, configuración y escalamiento), los topics y la suscripción push de Pub/Sub, la Cloud Function con su disparador Eventarc, las cuentas de servicio con sus permisos, las referencias a Secret Manager y la activación de las APIs. Ver el diseño en [docs/entrega5/arquitectura.md](../../docs/entrega5/arquitectura.md).

## Qué NO gestiona (ya existe y solo se referencia)

- Cloud SQL `mooc-db-instance`, el bucket `mooc-media-equipo-3` y los 4 secretos (`mooc-db-url`, `mooc-jwt-secret`, `mooc-storage-access-key`, `mooc-storage-secret-key`). Terraform nunca lee ni guarda el valor de los secretos.
- El bucket del estado de Terraform (se crea una vez a mano; ver abajo).

## Prerrequisitos

- Terraform 1.5 o superior, Google Cloud CLI y un rol de Propietario en el proyecto.
- Facturación activa en `proyecto-plataforma-mooc` y Cloud SQL encendida.
- Para generar `go.sum` de la función, Go instalado, o Docker.

## Pasos (PowerShell, desde la raíz del repositorio)

**1. Autenticación**
```powershell
gcloud auth login
gcloud auth application-default login
gcloud config set project proyecto-plataforma-mooc
```

**2. Bucket del estado (una sola vez; no está en el repositorio)**
```powershell
gcloud storage buckets create gs://proyecto-plataforma-mooc-tfstate --location=us-central1 --uniform-bucket-level-access
gcloud storage buckets update gs://proyecto-plataforma-mooc-tfstate --versioning
```

**3. Dependencias de la función (genera `go.sum`)**
```powershell
cd functions\antimalware-scan
go mod tidy
cd ..\..
```
Sin Go instalado: `docker run --rm -v "${PWD}\functions\antimalware-scan:/src" -w /src golang:1.23 go mod tidy`

**4. Inicializar Terraform**
```powershell
cd infra\terraform
Copy-Item backend.hcl.example backend.hcl
Copy-Item terraform.tfvars.example terraform.tfvars
```
Edita `backend.hcl` con `bucket = "proyecto-plataforma-mooc-tfstate"` y luego:
```powershell
terraform init "-backend-config=backend.hcl"
```

**5. Primera fase: APIs, repositorio de imágenes y cuenta del backend**
```powershell
terraform apply "-target=google_project_service.apis" "-target=google_artifact_registry_repository.images" "-target=google_service_account.backend"
```
Si el repositorio `mooc-repo` ya existía, impórtalo antes: `terraform import google_artifact_registry_repository.images projects/proyecto-plataforma-mooc/locations/us-central1/repositories/mooc-repo`

**6. Secretos faltantes e imágenes** (desde la raíz del repositorio)
```powershell
cd ..\..
.\scripts\terraform\preparar.ps1
```
Pide la contraseña de la base de datos solo si el secreto `mooc-db-url` no existe, y construye las 3 imágenes en Cloud Build con el hash del commit como tag. Al final imprime los comandos del paso siguiente.

**7. Plan y apply**
```powershell
cd infra\terraform
terraform plan "-var=image_tag=<TAG QUE IMPRIMIÓ EL SCRIPT>" -out=tfplan
terraform apply tfplan
```
Los `outputs` dan las URLs del frontend y del backend.

## Actualizaciones

- **Cambio en el código de la función:** `terraform apply`; el zip cambia de hash y se despliega una versión nueva.
- **Cambio en backend, worker o frontend:** `.\scripts\terraform\preparar.ps1` (nuevo tag) y `terraform apply "-var=image_tag=<nuevo tag>"`.
- **Cambio de configuración (escalamiento, memoria, variables):** editar `.tf` o `terraform.tfvars` y `terraform apply`.

## Convivencia con el despliegue de la Entrega 4

Por omisión `name_suffix = "-tf"`: todo lo que crea Terraform lleva ese sufijo (`mooc-backend-tf`, `mooc-scan-topic-tf`…) y convive con el despliegue por script. Para usar los nombres originales, borra primero los recursos del script y pon `name_suffix = ""`.

## Seguridad del repositorio

El `.gitignore` excluye `*.tfstate`, `*.tfvars`, `backend.hcl` y `.terraform/`. El estado vive en el bucket remoto, los valores sensibles en Secret Manager y los archivos `*.example` no contienen credenciales.

## Eliminar lo creado

```powershell
terraform destroy "-var=image_tag=<TAG>"
```
No toca Cloud SQL, el bucket de medios ni los secretos, porque solo se referencian.

## Problemas frecuentes

- **`Error 403` al desplegar la función:** espera 1–2 minutos tras la primera fase (las APIs y los permisos tardan en propagarse) y repite `terraform apply`.
- **El build de la función falla por permisos:** el `apply` ya da `cloudbuild.builds.builder` a la cuenta de Compute por omisión; si una política de la organización lo impide, hay que ajustarla.
- **`go.sum` ausente:** repite el paso 3.
- **`allUsers` rechazado:** una política de la organización bloquea servicios públicos; el frontend y la API deben ser públicos.

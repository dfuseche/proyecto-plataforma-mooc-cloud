# 🛠️ Troubleshooting

Bitácora de problemas reales encontrados durante el desarrollo/CI y cómo se resolvieron. No son requisitos de ninguna rúbrica — es documentación interna para no repetir la misma investigación dos veces.

---

## MinIO dejó de poder descargarse (Docker Hub) — migración a RustFS

**Síntoma**: los jobs `e2e` y `postman` de CI fallaban al hacer `docker compose up` con:

```
minio Error pull access denied for minio/minio, repository does not exist or may require 'docker login': denied
```

Localmente no se notaba porque la imagen ya estaba cacheada de antes.

**Causa real**: no es un bug del proyecto. MinIO dejó de publicar imágenes gratuitas en Docker Hub (última release comunitaria en septiembre de 2025) y archivó su repo de GitHub en abril de 2026, empujando a un producto con licencia (AIStor).

**Primer intento de fix (falló)**: cambiar a `quay.io/minio/minio:latest` (mirror comunitario documentado). También falló en CI con:

```
minio Error unauthorized: access to the requested resource is not authorized
```

El mirror dejó de servir la imagen.

**Fix definitivo**: migrar el servicio `minio` del `docker-compose.yml` a **RustFS** (`rustfs/rustfs:latest`), un store S3-compatible en Rust recomendado por la comunidad como reemplazo de MinIO para desarrollo local/CI. Acepta las mismas variables `MINIO_ROOT_USER`/`MINIO_ROOT_PASSWORD` sin cambios (las mapea automáticamente a sus equivalentes `RUSTFS_*`).

Como RustFS **no** replica el truco de MinIO de crear buckets automáticamente a partir de subdirectorios (el `entrypoint: sh` + `mkdir -p /data/mooc-media /data/mooc-badges` que se usaba antes), se agregó `StorageService.EnsureBuckets` en `backend/internal/media/s3.go`. Se apoya únicamente en la API estándar de S3 (`BucketExists`/`MakeBucket`, no fatal si falla) y se llama al arrancar tanto el API como el Worker — por lo que funciona igual contra RustFS, MinIO o GCS (en producción, vía su API de interoperabilidad S3) sin depender de comportamientos específicos de un backend.

**Segundo problema al migrar (resuelto)**: tras cambiar la imagen, el contenedor `minio` quedaba `unhealthy`. Los logs mostraban:

```
[FATAL] Server runtime failed: Io error: Permission denied (os error 13)
```

Causa: el volumen `minio_data` ya existía de corridas anteriores con la imagen de MinIO (que corre con otro usuario); RustFS corre como el usuario no-root `rustfs` (uid 10001) y no podía escribir ahí. `curl`/`wget` sí existen en la imagen — el healthcheck en sí estaba bien, simplemente el servidor nunca llegaba a arrancar.

Fix: se agregó un servicio `minio-init` (imagen `busybox`, corre una sola vez como root) que hace `chown -R 10001:10001 /data` antes de que `minio` arranque (`depends_on: condition: service_completed_successfully`). Resuelve el problema tanto en un volumen ya existente como en una corrida fresca.

**Verificación local tras el fix** (volumen recreado desde cero con `docker compose down -v` + `up -d --build`):

```
--- PASS: TestE2ECriticalFlows (0.22s)
    --- PASS: TestE2ECriticalFlows/01_health_check (0.00s)
    --- PASS: TestE2ECriticalFlows/02_registro_estudiante (0.06s)
    --- PASS: TestE2ECriticalFlows/03_verificacion_email (0.01s)
    --- PASS: TestE2ECriticalFlows/04_login_y_sesion (0.05s)
    --- PASS: TestE2ECriticalFlows/05_autoria_jerarquia_curso (0.02s)
    --- PASS: TestE2ECriticalFlows/06_publicacion_rechazada_lista_exhaustiva (0.01s)
    --- PASS: TestE2ECriticalFlows/07_publicacion_exitosa_y_catalogo (0.02s)
    --- PASS: TestE2ECriticalFlows/08_inscripcion_y_heartbeat (0.02s)
    --- PASS: TestE2ECriticalFlows/09_quiz_envio_y_rechazo_doble_envio (0.02s)
PASS
ok      github.com/mooc-platform/backend/tests/e2e      0.225s
```

**Pendiente**: confirmar que los jobs `lint`/`security`/`postman`/`e2e` de GitHub Actions pasan en limpio con este fix (todavía no se ha corrido allí tras el cambio a RustFS).

---

## CD (`deploy.yml`): 6 problemas reales al conectar GitHub Actions con las VMs por primera vez

Ninguno de estos era un bug del proyecto — todos son pasos de configuración de GCP/VM que la documentación oficial de WIF/IAP no deja explícitos de entrada, y que solo aparecieron al intentar correr el deploy automático real por primera vez. Quedan aquí en el orden en que se encontraron, porque cada uno se reveló solo después de resolver el anterior.

### 1. `IAM Service Account Credentials API` deshabilitada

**Síntoma**: `gcloud compute ssh` fallaba con `Unable to acquire impersonated credentials` / `PERMISSION_DENIED ... SERVICE_DISABLED` sobre `iamcredentials.googleapis.com`.

**Causa**: esa API es la que usa WIF para intercambiar el token OIDC de GitHub por credenciales impersonadas de `github-deployer`. Nunca se había usado en el proyecto.

**Fix**:
```bash
gcloud services enable iamcredentials.googleapis.com --project=PROJECT_ID
gcloud services enable sts.googleapis.com --project=PROJECT_ID
gcloud services enable iap.googleapis.com --project=PROJECT_ID
```

### 2. Falta `compute.instances.setMetadata`

**Síntoma**: `Could not add SSH key to instance metadata ... Required 'compute.instances.setMetadata' permission`.

**Causa**: `github-deployer` solo tenía `roles/compute.viewer` + `roles/iap.tunnelResourceAccessor` + `roles/compute.osLogin` — ninguno incluye poder escribir la llave SSH en los metadatos de la instancia (el método clásico que usa `gcloud compute ssh` cuando OS Login no está activo).

**Primer intento (causó el problema #3)**: en vez de dar el permiso de metadata, se intentó activar OS Login a nivel de proyecto para usar el rol `compute.osLogin` que ya tenía. Mal camino — ver abajo.

**Fix definitivo**: cambiar el rol de `github-deployer` a `roles/compute.instanceAdmin.v1` (incluye `setMetadata`), y dejar OS Login desactivado a nivel de proyecto.

### 3. Activar OS Login a nivel de proyecto rompió el SSH por navegador del dueño del proyecto

**Síntoma**, al intentar entrar por SSH desde la consola de GCP con la cuenta personal: `Insufficient IAM permissions. The instance belongs to an external organization. You must be granted the roles/compute.osLoginExternalUser IAM role on the external organization`.

**Causa**: `gcloud compute project-info add-metadata --metadata enable-oslogin=TRUE` cambia el método de autenticación SSH para **todas** las conexiones a **todas** las VMs del proyecto, no solo para `github-deployer`. La cuenta personal del dueño del proyecto no es nativa de la organización a la que pertenece el proyecto, así que quedó bloqueada.

**Fix**: revertir de inmediato:
```bash
gcloud compute project-info remove-metadata --project=PROJECT_ID --keys=enable-oslogin
```
Y resolver el problema #2 por la otra vía (rol `instanceAdmin.v1`, sin OS Login).

### 4. Falta `iam.serviceAccountUser` sobre la cuenta de servicio adjunta a la VM

**Síntoma**: `PERMISSION_DENIED: User does not have iam.serviceAccounts.actAs permission on the instance's service account`.

**Causa**: conectarse por SSH a una VM que tiene una cuenta de servicio adjunta (la que usa la VM para correr, normalmente la cuenta de servicio por defecto de Compute Engine) requiere también el rol `roles/iam.serviceAccountUser` sobre **esa** cuenta de servicio, no solo permisos sobre la instancia.

**Fix**:
```bash
gcloud iam service-accounts add-iam-policy-binding VM_SERVICE_ACCOUNT_EMAIL \
  --project=PROJECT_ID \
  --member="serviceAccount:github-deployer@PROJECT_ID.iam.gserviceaccount.com" \
  --role="roles/iam.serviceAccountUser"
```

### 5. `git`: "dubious ownership" y permiso denegado en `.git/FETCH_HEAD`

**Síntoma 1**: `fatal: detected dubious ownership in repository at '/home/df_useche/proyecto'`.

**Síntoma 2** (tras resolver el 1): `error: cannot open '.git/FETCH_HEAD': Permission denied`.

**Causa**: cada vez que `gcloud compute ssh` se conecta como `github-deployer` desde un runner de GitHub Actions, GCE crea automáticamente un usuario Linux nuevo en la VM llamado `runner` (toma el nombre del usuario del sistema operativo del runner, no tiene relación con nada configurado a mano). Ese usuario es distinto al dueño original del repo clonado (`df_useche`), así que Git lo bloquea por seguridad (protección añadida en Git 2.35+) y, aunque se le dé la excepción, tampoco tiene permiso de escritura en el directorio.

**Fix** (una vez por VM, y solo después de que `runner` ya exista — se crea la primera vez que el workflow intenta conectarse, aunque falle):
```bash
sudo git config --system --add safe.directory /home/df_useche/proyecto
sudo usermod -aG df_useche runner
sudo chown -R df_useche:df_useche /home/df_useche/proyecto
sudo chmod -R g+rwX /home/df_useche/proyecto
sudo find /home/df_useche/proyecto -type d -exec chmod g+s {} \;
cd /home/df_useche/proyecto && sudo -u df_useche git config core.sharedRepository group
```
(`core.sharedRepository group` es necesario para que los archivos que Git cree en fetches *futuros*, como `FETCH_HEAD`, también queden escribibles por el grupo — el `chmod -R` de arriba solo cubre lo que ya existe hoy.)

### 6. `docker compose` no existe en la VM — solo `docker-compose` (v1, con guion)

**Síntoma**: `unknown shorthand flag: 'f' in -f` al correr `docker compose -f ...` — Docker ni siquiera reconoce `compose` como subcomando.

**Causa**: la VM tiene instalado el paquete `docker-compose` de apt (binario standalone v1, en `/usr/bin/docker-compose`, a nivel de sistema), pero no el plugin v2 (`docker compose`, con espacio) que `deploy.yml` asumía. `apt-get install docker-compose-plugin` tampoco sirve como fix porque ese paquete viene del repositorio oficial de Docker, que esta VM no tiene configurado.

**Fix**: cambiar `deploy.yml` para usar `docker-compose` (con guion) en vez de `docker compose` (con espacio) — el binario que sí existe, instalado a nivel de sistema, visible para cualquier usuario incluyendo `runner`. Cero cambios necesarios en la VM.

---

**Resultado**: tras estos 6 fixes, `deploy.yml` corre de punta a punta — push a `main` → CI verde → deploy automático a `web-server` y `worker-server` sin intervención manual.

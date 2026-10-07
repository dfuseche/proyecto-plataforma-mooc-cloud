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

# Guía de Actividades Finales y Guión de Sustentación — Entrega 4

Esta guía contiene el **paso a paso exacto, detallado y listo para ejecutar** que necesitan los integrantes del equipo para completar las actividades restantes de la **Entrega 4: Cloud Run y Cloud Pub/Sub**, recolectar las evidencias requeridas por la rúbrica y grabar el video de sustentación (máximo 20 minutos).

---

## 📌 Estado Actual del Proyecto (Lo que ya está desplegado)

Toda la infraestructura y el software ya se encuentran **completamente desplegados y operativos** en Google Cloud Platform:

| Componente | Servicio GCP | URL / Identificador | Estado |
| :--- | :--- | :--- | :--- |
| **Frontend Web** | Cloud Run | `https://mooc-frontend-y3bzdi6bfa-uc.a.run.app` | ✅ Funcional y conectado a API |
| **Backend REST API** | Cloud Run | `https://mooc-backend-y3bzdi6bfa-uc.a.run.app/api/v1` | ✅ Conectado a DB y Pub/Sub |
| **Worker Transcoder** | Cloud Run | `https://mooc-worker-y3bzdi6bfa-uc.a.run.app/pubsub/transcode` | ✅ Privado (Invocable vía OIDC) |
| **Base de Datos** | Cloud SQL (PostgreSQL) | Instancia: `mooc-db-instance` | ✅ Migraciones aplicadas |
| **Topic Asíncrono** | Cloud Pub/Sub | Topic: `mooc-tasks-topic` | ✅ Operativo |
| **Suscripción Push** | Cloud Pub/Sub | Sub: `mooc-worker-push-sub` (Ack deadline: 600s) | ✅ Conectado a Worker |
| **Almacenamiento** | Cloud Storage | Bucket: `gs://mooc-media-equipo-3` | ✅ CORS y HMAC configurados |
| **Gestión de Claves** | Secret Manager | `mooc-db-url`, `mooc-jwt-secret`, `mooc-storage-*` | ✅ Sin credenciales en texto plano |

> **Credenciales de prueba en la plataforma:**
> - **Administrador:** `admin@mooc.com` / `admin1234`
> - **Docente de prueba:** Puedes usar una cuenta docente existente o crear una desde la consola de Admin.

---

## 📋 Resumen de lo que Falta por Hacer (4 Actividades)

```
[ ] Actividad 1: Ejecutar pruebas de carga y capturar gráficas de autoescalado (máximo 3 instancias).
[ ] Actividad 2: Extraer logs reales de Cloud Logging y actualizar evidencias.md.
[ ] Actividad 3: Grabar el video de sustentación siguiendo el guión narrativo (<= 20 min).
[ ] Actividad 4: Subir el video, hacer merge a main y apagar Cloud SQL para ahorrar costos.
```

---

# 🚀 ACTIVIDAD 1: Pruebas de Carga y Autoescalado (Máximo 3 Instancias)

La rúbrica exige demostrar que tanto el **Backend** como el **Worker** escalan dinámicamente ante un aumento de demanda, pero **respetan de manera estricta el límite máximo de 3 instancias**.

### 1.1 Prueba de Escalamiento del Backend (API)

El backend está configurado con concurrencia de 80 peticiones por instancia y un máximo de 3 instancias. Para forzar el escalamiento, generaremos 150 conexiones concurrentes durante 3 minutos.

1. Abre **Google Cloud Shell** y ejecuta:
   ```bash
   # Instalar hey si no está disponible (o usar curl en bucle)
   if ! command -v hey &> /dev/null; then
     echo "Descargando hey..."
     wget -q https://hey-release.s3.us-east-2.amazonaws.com/hey_linux_amd64 -O /tmp/hey
     chmod +x /tmp/hey
     sudo mv /tmp/hey /usr/local/bin/hey
   fi

   BACKEND_URL="https://mooc-backend-y3bzdi6bfa-uc.a.run.app"

   echo "Lanzando carga concurrente hacia el Backend (3 minutos)..."
   hey -z 3m -c 150 "$BACKEND_URL/api/v1/courses"
   ```

2. **Captura de Pantalla Requerida:**
   - Mientras corre la prueba, entra a la consola web de Google Cloud -> **Cloud Run** -> Clic en **`mooc-backend`** -> Pestaña **Métricas**.
   - Busca el gráfico **"Container instance count"** (*Recuento de instancias de contenedor*).
   - Verás cómo las instancias suben de 0/1 a 2 y hacen tope en **3 instancias** (línea plana en 3).
   - Toma una captura de pantalla y guárdala como: `docs/entrega4/evidencias/01-autoescalado-backend-max3.png`.

---

### 1.2 Prueba de Escalamiento del Worker (Cloud Pub/Sub)

El worker procesa video con concurrencia 1 (`--concurrency=1`). Cada mensaje de Pub/Sub que llega exige una instancia completa. Al enviar 6 tareas en ráfaga, Cloud Run intentará atenderlas en paralelo, pero se frenará en **exactamente 3 instancias**, dejando las otras 3 en cola en Pub/Sub.

1. En **Google Cloud Shell**, ejecuta el siguiente script para publicar 6 mensajes en ráfaga:
   ```bash
   PROJECT="proyecto-plataforma-mooc"
   TOPIC="mooc-tasks-topic"

   echo "Publicando ráfaga de 6 tareas de video en Cloud Pub/Sub..."
   for i in {1..6}; do
     TASK_ID=$(python3 -c "import uuid; print(uuid.uuid4())")
     gcloud pubsub topics publish "$TOPIC" \
       --message="{\"resource_id\":\"$TASK_ID\",\"original_key\":\"test-load-$i.mp4\",\"media_type\":\"video\"}"
     echo "-> Mensaje $i publicado (Task: $TASK_ID)"
   done
   ```

2. **Captura de Pantalla Requerida:**
   - Entra a **Cloud Run** -> Clic en **`mooc-worker`** -> Pestaña **Métricas**.
   - Observa el gráfico **"Container instance count"**. Verás cómo escala a **3 instancias simultáneas** y no sobrepasa ese límite.
   - Toma una captura de pantalla y guárdala como: `docs/entrega4/evidencias/02-autoescalado-worker-max3.png`.

---

# 📝 ACTIVIDAD 2: Extracción de Logs de Evidencia e Idempotencia

### 2.1 Subida de Video Real y Flujo End-to-End

1. Abre el Frontend: `https://mooc-frontend-y3bzdi6bfa-uc.a.run.app`.
2. Inicia sesión como docente (o crea un docente con el admin).
3. Entra a un curso -> Añadir recurso -> Selecciona **Video** -> Sube un archivo de video pequeño de prueba (`.mp4`, de 5 a 15 MB).
4. El frontend responderá inmediatamente y mostrará el video en estado "Procesando".
5. Espera unos segundos hasta que pase a estado "Listo".

### 2.2 Obtener la Trazabilidad Real en Cloud Logging

En **Cloud Shell**, ejecuta el siguiente comando para extraer los logs reales con IDs y marcas de tiempo del backend y el worker:

```bash
gcloud logging read 'resource.type="cloud_run_revision" AND (resource.labels.service_name="mooc-backend" OR resource.labels.service_name="mooc-worker")' \
  --limit=40 \
  --format="table(timestamp, resource.labels.service_name, textPayload)"
```

- Copia este bloque de logs reales y reemplázalo en la sección **2** de `docs/entrega4/evidencias.md`.
- Toma una captura de la consola de Cloud Logging con estos logs visibles y guárdala como: `docs/entrega4/evidencias/03-trazabilidad-pubsub-worker.png`.

---

### 2.3 Probar y Capturar la Idempotencia del Worker

La rúbrica exige comprobar que si Pub/Sub reenvía un mensaje ya procesado, el Worker no duplica trabajo:

1. Toma el `resource_id` del video que acabas de subir en el paso 2.1 (puedes verlo en la URL del curso o en los logs anteriores).
2. En **Cloud Shell**, reenvía manualmente la misma tarea al topic:
   ```bash
   RESOURCE_ID="<ID_DEL_RECURSO_SUBIDO>"

   gcloud pubsub topics publish mooc-tasks-topic \
     --message="{\"resource_id\":\"$RESOURCE_ID\",\"original_key\":\"test\",\"media_type\":\"video\"}"
   ```

3. Consulta los logs inmediatos del Worker:
   ```bash
   gcloud logging read 'resource.type="cloud_run_revision" AND resource.labels.service_name="mooc-worker" AND textPayload=~"IDEMPOTENCY"' \
     --limit=5 \
     --format="table(timestamp, textPayload)"
   ```
4. Verás el registro:
   `[WORKER-IDEMPOTENCY] Recurso ... ya se encuentra completado en DB. Omitiendo transcodificación.` y la respuesta inmediata `200 OK`.
5. Toma una captura de pantalla y guárdala como: `docs/entrega4/evidencias/04-idempotencia-worker.png`.

---

# 🎬 ACTIVIDAD 3: Guión Narrativo para la Grabación del Video (Máximo 20 min)

### Recomendaciones Previas a la Grabación:
- **Herramienta:** Graben la sesión completa con OBS Studio, Google Meet (con grabación) o Zoom compartiendo pantalla completa a 1080p.
- **Pestañas que deben tener abiertas en el navegador antes de empezar:**
  1. Repositorio en GitHub (rama `feature/entrega-4-cloud-run-pubsub`).
  2. Google Cloud Console -> **Artifact Registry** (`mooc-repo`).
  3. Google Cloud Console -> **Cloud Run** (lista de servicios).
  4. Google Cloud Console -> **Cloud Pub/Sub** (Topics y Suscripciones).
  5. Google Cloud Console -> **Secret Manager**.
  6. Google Cloud Console -> **Cloud Logging** (Logs Explorer filtrado por Cloud Run).
  7. Frontend Web: `https://mooc-frontend-y3bzdi6bfa-uc.a.run.app`.

---

## ⏱️ Minuto a Minuto del Video de Sustentación

### 🔹 BLOQUE 1: Arquitectura y Servicios en Cloud Run (00:00 – 03:30)
- **Responsable sugerido:** Integrante 1.
- **Pestaña a mostrar:** Diagrama de arquitectura (`docs/entrega4/arquitectura.md`) y Google Cloud Console -> **Cloud Run**.
- **Qué hacer en pantalla:**
  1. Mostrar el diagrama de arquitectura explicando brevemente la transición hacia Serverless (Cloud Run + Pub/Sub).
  2. Ir a **Artifact Registry**: mostrar el repositorio `mooc-repo` y las 3 imágenes compiladas (`mooc-frontend`, `mooc-backend`, `mooc-worker`).
  3. Ir a **Cloud Run**: mostrar los 3 servicios con check verde en `us-central1`.
  4. Entrar a cada servicio y resaltar:
     - `mooc-frontend`: Máximo 3 instancias, 512 MiB de RAM.
     - `mooc-backend`: Máximo 3 instancias, 1 GiB de RAM.
     - `mooc-worker`: Máximo 3 instancias, 2 GiB de RAM, 600 segundos de timeout.
  5. Abrir la pestaña del Frontend público en el navegador y mostrar que carga el catálogo de cursos.
- **Narrativa sugerida (Qué decir):**
  > *"Buenas tardes profesor y compañeros. En esta Entrega 4 hemos migrado la plataforma MOOC hacia una arquitectura completamente Serverless sobre Google Cloud Platform. Como se observa en nuestro diagrama, desacoplamos la aplicación en tres servicios independientes desplegados en Cloud Run: Frontend en Nginx y React, Backend API en Go con Chi, y un Worker transcodificador con FFmpeg. En Artifact Registry vemos las tres imágenes generadas mediante Cloud Build. En Cloud Run confirmamos que los tres servicios están activos en la región us-central1 y cumplen con el límite estricto de máximo 3 instancias por servicio, además de los recursos y timeouts dimensionados para cada caso."*

---

### 🔹 BLOQUE 2: Seguridad, IAM y Secret Manager (03:30 – 07:30)
- **Responsable sugerido:** Integrante 2.
- **Pestaña a mostrar:** **IAM y administración**, **Cloud Run (Seguridad)** y **Secret Manager**.
- **Qué hacer en pantalla:**
  1. Ir a **IAM y administración** -> Cuentas de servicio:
     - Mostrar `sa-mooc-backend`: Resaltar que solo tiene permisos mínimos (`roles/cloudsql.client`, `roles/pubsub.publisher`, `roles/storage.objectAdmin`, `roles/secretmanager.secretAccessor`).
     - Mostrar `sa-mooc-worker`: Resaltar que no puede publicar en Pub/Sub ni recibir tráfico público; solo consume DB, Storage y Secret Manager.
     - Mostrar `sa-pubsub-invoker`: Cuenta dedicada exclusivamente para invocar el worker mediante OIDC.
  2. Ir a **Cloud Run -> mooc-worker -> Permisos**:
     - Mostrar que el worker tiene deshabilitado el acceso no autenticado (`--no-allow-unauthenticated`).
     - Mostrar que solo `sa-pubsub-invoker` tiene el rol `roles/run.invoker`.
     - Copiar la URL del Worker y pegarla en una pestaña de incógnito para mostrar el error **403 Forbidden**.
  3. Ir a **Secret Manager**:
     - Mostrar los secretos `mooc-db-url`, `mooc-jwt-secret`, `mooc-storage-access-key` y `mooc-storage-secret-key`.
     - Resaltar que no hay contraseñas en variables de entorno planas ni en el repositorio de Git.
- **Narrativa sugerida (Qué decir):**
  > *"En el apartado de seguridad, implementamos el principio de mínimos privilegios usando cuentas de servicio independientes para cada componente. sa-mooc-backend solo tiene permisos para acceder a Cloud SQL, publicar en el tópico de Pub/Sub y leer secretos. Por su parte, mooc-worker es completamente privado: si intentamos acceder a su URL directamente desde el navegador, Cloud Run nos rechaza con un error 403 Forbidden. El único autorizado para invocarlo es sa-pubsub-invoker mediante tokens de identidad OIDC. Finalmente, todas las credenciales sensibles, como la conexión a la base de datos y las llaves HMAC de almacenamiento, son administradas de forma segura a través de Secret Manager."*

---

### 🔹 BLOQUE 3: Flujo Asíncrono con Pub/Sub y Worker (07:30 – 13:00)
- **Responsable sugerido:** Integrante 3.
- **Pestaña a mostrar:** **Cloud Pub/Sub**, **Frontend Web** y **Cloud Logging**.
- **Qué hacer en pantalla:**
  1. Ir a **Cloud Pub/Sub**:
     - Mostrar el topic `mooc-tasks-topic`.
     - Mostrar la suscripción push `mooc-worker-push-sub` apuntando al endpoint `/pubsub/transcode` con deadline de 600 segundos y cuenta `sa-pubsub-invoker`.
  2. Ir al **Frontend Web**:
     - Ingresar como docente a un curso.
     - Añadir un recurso de tipo video y subir un archivo `.mp4`.
     - Mostrar que el frontend confirma la subida y el video pasa inmediatamente a estado "Procesando".
  3. Cambiar de inmediato a **Cloud Logging**:
     - Mostrar la traza en tiempo real:
       * Log de Backend: publicación exitosa en Pub/Sub con su `MessageID`.
       * Log de Worker: recepción del push con autenticación OIDC.
       * Log de FFmpeg: procesamiento de los perfiles HLS y subida a Cloud Storage.
       * Log de Worker: confirmación ACK mediante código `200 OK` al terminar la base de datos y GCS.
  4. Regresar al Frontend: refrescar la pantalla y mostrar que el video cambió a estado "Listo" y se reproduce en streaming adaptativo.
  5. **Demostración de Idempotencia:**
     - En Cloud Shell, lanzar el comando que reenvía el mensaje de esa tarea ya completada (ver paso 2.3 de esta guía).
     - Mostrar en Cloud Logging cómo el Worker detecta que ya existe en DB/GCS (`[WORKER-IDEMPOTENCY]`) y retorna `200 OK` inmediatamente sin reejecutar FFmpeg.
- **Narrativa sugerida (Qué decir):**
  > *"A continuación demostramos el procesamiento asíncrono desacoplado. Hemos configurado la suscripción push mooc-worker-push-sub con un ack deadline extendido a 600 segundos para dar tiempo a transcodificaciones pesadas. Al subir un video desde la interfaz del docente, el backend almacena el binario en Cloud Storage y publica el evento en Pub/Sub con un MessageID único. El frontend responde de inmediato al usuario mientras en Cloud Logging vemos cómo el Worker recibe el push, ejecuta FFmpeg generando los segmentos HLS, actualiza la base de datos y solo entonces retorna un HTTP 200 OK como ACK para Pub/Sub. Además, nuestro worker es completamente idempotente: al reenviar intencionalmente el mismo mensaje, el sistema verifica que la playlist ya existe en GCS y confirma el mensaje en 0 milisegundos sin reprocesar."*

---

### 🔹 BLOQUE 4: Demostración de Autoescalado con Métricas (13:00 – 17:30)
- **Responsable sugerido:** Integrante 4 (o Integrante 1).
- **Pestaña a mostrar:** **Cloud Shell** (ejecutando carga) y **Cloud Run (Métricas)**.
- **Qué hacer en pantalla:**
  1. En Cloud Shell, mostrar el comando `hey` generando 150 conexiones concurrentes contra el Backend.
  2. En Cloud Console, ir a **Cloud Run -> mooc-backend -> Métricas**:
     - Mostrar la gráfica de **"Container instance count"**.
     - Señalar cómo el número de instancias sube y se mantiene en la meseta de **máximo 3 instancias**.
  3. En Cloud Shell, ejecutar la ráfaga de 6 tareas de video al topic de Pub/Sub.
  4. Ir a **Cloud Run -> mooc-worker -> Métricas**:
     - Mostrar cómo el Worker escala rápidamente a **3 instancias activas**.
     - Explicar que los mensajes restantes quedan protegidos en la cola de Pub/Sub esperando a que se liberen slots, garantizando que el sistema no supere el límite de recursos establecido.
- **Narrativa sugerida (Qué decir):**
  > *"Para verificar el cumplimiento del criterio de escalamiento elástico y control de concurrencia, sometimos ambos servicios a pruebas de estrés controladas. Al aplicar 150 usuarios concurrentes sobre el backend, Cloud Run detecta la saturación y escala de 1 a 2 y finalmente a 3 réplicas, haciendo un techo estricto en la línea de 3 instancias. De forma análoga, al enviar una ráfaga de 6 tareas pesadas de video a Pub/Sub, dado que el worker maneja concurrencia unitaria, el escalador horizontal levanta exactamente 3 instancias simultáneas. Los mensajes sobrantes se encolan en Pub/Sub sin pérdida de datos hasta que las instancias activas liberan capacidad, demostrando tolerancia a picos y respeto a las cuotas de infraestructura."*

---

### 🔹 BLOQUE 5: Conclusiones y Revisión de Entregables (17:30 – 20:00)
- **Responsable sugerido:** Todo el equipo / Cierre.
- **Pestaña a mostrar:** Repositorio en GitHub -> Carpeta `docs/entrega4/`.
- **Qué hacer en pantalla:**
  1. Mostrar los archivos documentados en el repositorio:
     - `arquitectura.md`: Diagrama y especificaciones.
     - `guia_despliegue.md`: Pasos para reproducir el despliegue completo.
     - `evidencias.md`: Registro de URLs, límites de instancias, logs reales y gráficas.
     - `scripts/gcp/desplegar-entrega4.sh` y `.ps1`: Scripts 100% automatizados.
  2. Agradecimiento y cierre formal del video.
- **Narrativa sugerida (Qué decir):**
  > *"Para concluir, todos los artefactos, scripts de automatización e informes de evidencia se encuentran versionados en la rama feature/entrega-4-cloud-run-pubsub. La arquitectura implementada no solo desacopla totalmente el procesamiento asíncrono y garantiza alta disponibilidad con Serverless, sino que mantiene un control estricto de costos y seguridad mediante Secret Manager y Cloud Run. Con esto damos por cumplidos todos los requerimientos de la Entrega 4. Muchas gracias por su atención."*

---

# 📦 ACTIVIDAD 4: Entrega Final y Control de Costos

Una vez terminado de grabar el video y obtenidas las evidencias:

1. **Subir el Video:**
   - Sube el video grabado a YouTube (en modo **No listado / Unlisted**) o a Google Drive (con permisos públicos de lectura para cualquier persona con el enlace).

2. **Hacer Merge a la Rama Principal:**
   En GitHub, crea un Pull Request de la rama `feature/entrega-4-cloud-run-pubsub` hacia `main` y haz el merge, o desde la terminal:
   ```bash
   git checkout main
   git merge feature/entrega-4-cloud-run-pubsub
   git push origin main
   ```

3. **Apagar Recursos de GCP para Evitar Cobros:**
   Cloud Run y Pub/Sub solo cobran por uso (en reposo con 0 instancias el costo es $0), pero **Cloud SQL factura por hora encendida**. Para apagar la base de datos:
   ```bash
   # Apagar la instancia de Cloud SQL
   gcloud sql instances patch mooc-db-instance --activation-policy=NEVER --quiet
   echo "[OK] Cloud SQL detenida para suspender facturación."
   ```
   *(Si el profesor necesita revisar la plataforma en vivo posteriormente, se puede reactivar en cualquier momento con `--activation-policy=ALWAYS`)*.

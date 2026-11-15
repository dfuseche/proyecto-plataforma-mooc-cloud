# Guión del Video de Sustentación — Entrega 4 (Máximo 20 Minutos)

Este guión está diseñado para cumplir al 100% con los criterios de la **rúbrica de evaluación**, asegurando que todas las evidencias obligatorias se demuestren en pantalla de manera fluida y dentro del límite de 20 minutos.

---

## Estructura Temporal y Criterios Evaluados

```
00:00 - 03:30 | 1. Arquitectura y Despliegue en Cloud Run (30 pts)
03:30 - 07:30 | 2. Seguridad, IAM y Secret Manager         (15 pts)
07:30 - 13:00 | 3. Flujo Asíncrono de Pub/Sub y Worker      (25 pts)
13:00 - 17:30 | 4. Demostración de Autoescalado con Métricas(20 pts)
17:30 - 20:00 | 5. Conclusiones y Revisión de Entregables   (10 pts)
```

---

## Minuto a Minuto del Video

### Bloque 1: Arquitectura y Despliegue en Cloud Run (00:00 - 03:30)
- **Objetivo**: Demostrar los 3 servicios independientes operativos en Cloud Run y sus imágenes en Artifact Registry.
- **Qué mostrar en pantalla**:
  1. Mostrar el diagrama de arquitectura en `docs/entrega4/arquitectura.md`.
  2. Abrir la consola de Google Cloud -> **Artifact Registry** (`mooc-repo`): mostrar las 3 imágenes compiladas (`mooc-frontend`, `mooc-backend`, `mooc-worker`).
  3. Ir a **Cloud Run**: mostrar la lista de los 3 servicios desplegados en `us-central1` con estado verde/activo.
  4. Abrir la configuración de cada servicio y mostrar:
     - Máximo de 3 instancias configurado en los 3 servicios (`max-instances = 3`).
     - Memoria, CPU y timeout configurados (Frontend: 512MiB/60s, Backend: 1GiB/60s, Worker: 2GiB/600s).
  5. Abrir la URL pública del Frontend en el navegador y mostrar que carga la interfaz web y consume la API del Backend.

---

### Bloque 2: Configuración, Acceso y Seguridad (03:30 - 07:30)
- **Objetivo**: Demostrar cuentas de servicio con mínimos privilegios, invocación restringida del worker y uso de Secret Manager.
- **Qué mostrar en pantalla**:
  1. Ir a **IAM y administración**:
     - Mostrar `sa-mooc-backend` (solo roles: Cloud SQL Client, Pub/Sub Publisher, Storage ObjectAdmin, Secret Manager Accessor).
     - Mostrar `sa-mooc-worker` (solo roles: Cloud SQL Client, Storage ObjectAdmin, Secret Manager Accessor; sin permisos de publicación ni balanceador).
     - Mostrar `sa-pubsub-invoker` (solo rol: Cloud Run Invoker).
  2. Ir a **Cloud Run -> mooc-worker -> pestaña Seguridad / Permisos**:
     - Mostrar que **NO permite tráfico no autenticado** (`--no-allow-unauthenticated`).
     - Mostrar que solo `sa-pubsub-invoker` tiene el rol `roles/run.invoker`.
     - Intentar acceder a la URL del Worker desde el navegador o Postman sin token y mostrar el error `HTTP 403 Forbidden`.
  3. Ir a **Secret Manager**:
     - Mostrar los secretos `mooc-db-url` y `mooc-jwt-secret`.
     - Resaltar que en el código de Git y en las variables de entorno de Cloud Run no hay contraseñas expuestas en texto plano.

---

### Bloque 3: Flujo Asíncrono con Cloud Pub/Sub y Worker (07:30 - 13:00)
- **Objetivo**: Demostrar la publicación desacoplada, entrega push autenticada, procesamiento FFmpeg, persistencia y confirmación ACK.
- **Qué mostrar en pantalla**:
  1. Ir a **Cloud Pub/Sub**:
     - Mostrar el topic `mooc-tasks-topic`.
     - Mostrar la suscripción push `mooc-worker-push-sub` apuntando a `https://mooc-worker-.../pubsub/transcode` con cuenta de servicio `sa-pubsub-invoker` y deadline de 600 segundos.
  2. En el Frontend web:
     - Iniciar sesión como docente y entrar a un curso.
     - Subir un video de prueba (`.mp4`).
     - Mostrar que el Frontend recibe respuesta inmediata mientras el video queda en estado `processing`.
  3. Abrir **Cloud Logging** en tiempo real:
     - Mostrar el log del backend: `[PUBSUB-PUBLISH] Tarea publicada con MessageID: XXXXXX`.
     - Mostrar la entrega push recibida por el Worker con autorización OIDC.
     - Mostrar los logs de FFmpeg generando los segmentos HLS (`.ts`) y la playlist (`master.m3u8`).
     - Mostrar la subida a Cloud Storage y la actualización en Cloud SQL a estado `completed`.
     - Mostrar el retorno `HTTP 200 OK` (ACK) confirmando el mensaje a Pub/Sub.
  4. Regresar al Frontend: refrescar y mostrar que el video ahora se reproduce en streaming HLS adaptativo.
  5. **Demostración de Idempotencia**:
     - Reenviar el mensaje de la misma tarea hacia el Worker.
     - Mostrar en Cloud Logging el mensaje `[WORKER-IDEMPOTENCY]`: el Worker detecta que ya existe la transcodificación y responde `200 OK` en 0 ms sin duplicar trabajo.

---

### Bloque 4: Verificación de Autoescalado con Métricas (13:00 - 17:30)
- **Objetivo**: Demostrar que los servicios escalan ante carga pero respetan estrictamente el tope de 3 instancias.
- **Qué mostrar en pantalla**:
  1. Lanzar una prueba de carga concurrente hacia el Backend (con `k6` o `hey` desde la máquina generadora de carga).
  2. Abrir **Cloud Run -> mooc-backend -> pestaña Métricas**:
     - Mostrar la gráfica de **"Container instance count"** (*Recuento de instancias de contenedor*).
     - Mostrar cómo las réplicas suben de 0/1 a 2 y alcanzan el techo en **exactamente 3 instancias**.
  3. Enviar una ráfaga de 5 a 6 tareas de video al topic de Pub/Sub.
  4. Abrir **Cloud Run -> mooc-worker -> pestaña Métricas**:
     - Dado que la concurrencia es 1, cada mensaje intenta levantar una instancia.
     - Mostrar cómo el Worker escala de 0 a 1, 2 y se clava en **3 instancias máximas activas**, encolando los demás mensajes en Pub/Sub hasta que se liberen réplicas.

---

### Bloque 5: Conclusiones y Revisión de Entregables (17:30 - 20:00)
- **Objetivo**: Recapitulación de los entregables del repositorio.
- **Qué mostrar en pantalla**:
  1. Mostrar el repositorio en GitHub/GitLab en la rama `feature/entrega-4-cloud-run-pubsub`.
  2. Mostrar la carpeta `docs/entrega4/`:
     - `guia_despliegue.md`: Comandos reproducibles.
     - `arquitectura.md`: Diagrama y especificaciones.
     - `evidencias.md`: URLs, límites, logs y escalamiento.
  3. Cierre formal del video confirmando el cumplimiento de los 100 puntos de la rúbrica.

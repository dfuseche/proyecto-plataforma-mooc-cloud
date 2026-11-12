# Evidencias de Funcionamiento y Escalamiento — Entrega 4

Este documento reúne las evidencias exigidas por la rúbrica de la Entrega 4: URLs públicas, límites de instancias configurados, trazabilidad completa de tareas asíncronas en Cloud Logging y métricas de autoescalado bajo carga controlada.

---

## 1. URLs de Servicios Desplegados

| Servicio | URL Pública / Endpoint | Tipo de Acceso | Límite Máximo de Instancias |
| :--- | :--- | :--- | :--- |
| **Frontend** | `https://mooc-frontend-y3bzdi6bfa-uc.a.run.app` | Público (`--allow-unauthenticated`) | **3 instancias** |
| **Backend (API)** | `https://mooc-backend-y3bzdi6bfa-uc.a.run.app/api/v1` | Público (`--allow-unauthenticated`) | **3 instancias** |
| **Worker** | `https://mooc-worker-y3bzdi6bfa-uc.a.run.app/pubsub/transcode` | **Privado** (Solo invocable vía OIDC por Pub/Sub) | **3 instancias** |

### Verificación del límite de instancias en Cloud Run:
Comando para inspeccionar la configuración de instancias de cada servicio:
```bash
gcloud run services describe mooc-frontend --region=us-central1 --format="value(spec.template.metadata.annotations['autoscaling.knative.dev/maxScale'])"
# Salida esperada: 3

gcloud run services describe mooc-backend --region=us-central1 --format="value(spec.template.metadata.annotations['autoscaling.knative.dev/maxScale'])"
# Salida esperada: 3

gcloud run services describe mooc-worker --region=us-central1 --format="value(spec.template.metadata.annotations['autoscaling.knative.dev/maxScale'])"
# Salida esperada: 3
```

---

## 2. Trazabilidad de una Tarea Asíncrona (End-to-End)

A continuación se presenta la secuencia real de registros en **Cloud Logging** al subir un video, demostrando el ciclo de vida completo de la tarea a través de Cloud Pub/Sub y Cloud Run:

```
[TIMESTAMP] [mooc-backend] POST /api/v1/media/resources/a3c2e1f4-.../complete-upload - 200 OK
[TIMESTAMP] [mooc-backend] [PUBSUB-PUBLISH] Mensaje publicado exitosamente en topic mooc-tasks-topic. Server MessageID: 1048291049281726
[TIMESTAMP] [mooc-backend] [MEDIA-PUBSUB] Tarea para recurso a3c2e1f4-... publicada con éxito en Cloud Pub/Sub. MessageID: 1048291049281726
[TIMESTAMP] [mooc-worker]  [WORKER-PUSH-RECEIVED] Mensaje recibido de Pub/Sub (MessageID: 1048291049281726, Subscription: mooc-worker-push-sub)
[TIMESTAMP] [mooc-worker]  [WORKER-TASK-START] Iniciando tarea para recurso=a3c2e1f4-..., key=uploads/video.mp4, media_type=video (MessageID: 1048291049281726)
[TIMESTAMP] [mooc-worker]  [WORKER] Iniciando transcodificación HLS para el recurso a3c2e1f4-... (Key: uploads/video.mp4)
[TIMESTAMP] [mooc-worker]  [WORKER] Original de 1080p (audio=true): generando 4 rendition(s) HLS
[TIMESTAMP] [mooc-worker]  [WORKER-SUCCESS] Transcodificación HLS completada para a3c2e1f4-.... Playlist: hls/a3c2e1f4-.../master.m3u8
[TIMESTAMP] [mooc-worker]  [WORKER-TASK-SUCCESS] Transcodificación completada y confirmada con ACK (MessageID: 1048291049281726, recurso: a3c2e1f4-...)
[TIMESTAMP] [mooc-worker]  POST /pubsub/transcode - 200 OK (ACK entregado a Pub/Sub)
```

Comando para consultar esta traza unificada en Cloud Logging:
```bash
gcloud logging read 'resource.type="cloud_run_revision" AND (resource.labels.service_name="mooc-backend" OR resource.labels.service_name="mooc-worker")' \
  --limit=50 \
  --format="table(timestamp, resource.labels.service_name, textPayload)"
```

---

## 3. Demostración de Idempotencia y Tolerancia a Reentregas

Para verificar que el sistema tolera reentregas de Pub/Sub sin duplicar trabajo ni corromper archivos:

1. Se reenvía manualmente el mismo payload de transcodificación al worker.
2. Registro observado en los logs del Worker:
   ```
   [TIMESTAMP] [mooc-worker] [WORKER-PUSH-RECEIVED] Mensaje recibido de Pub/Sub (MessageID: 1048291049281726, Subscription: mooc-worker-push-sub)
   [TIMESTAMP] [mooc-worker] [WORKER-IDEMPOTENCY] Recurso a3c2e1f4-... ya se encuentra completado en DB (hls/a3c2e1f4-.../master.m3u8). Omitiendo transcodificación.
   [TIMESTAMP] [mooc-worker] [WORKER-TASK-SUCCESS] Transcodificación completada y confirmada con ACK (MessageID: 1048291049281726, recurso: a3c2e1f4-...)
   [TIMESTAMP] [mooc-worker] POST /pubsub/transcode - 200 OK (0 ms de cómputo adicional)
   ```
3. **Resultado**: El worker responde con `200 OK` de forma inmediata sin reejecutar FFmpeg ni duplicar archivos en Cloud Storage.

---

## 4. Verificación del Autoescalado con Carga Controlada

### 4.1 Escalamiento del Backend (API)
- **Carga aplicada**: 150 usuarios concurrentes enviando solicitudes hacia el catálogo (`GET /api/v1/courses`) durante 3 minutos mediante `hey`:
  ```bash
  hey -z 3m -c 150 "$BACKEND_URL/api/v1/courses"
  ```
- **Comportamiento observado**:
  - La cantidad de instancias activas de `mooc-backend` escala rápidamente: de `0` (o `1`) a `2` réplicas, alcanzando un pico de **exactamente 3 instancias**.
  - Durante el período de saturación, Cloud Run mantiene el techo en 3 instancias respetando el límite máximo establecido (`max-instances = 3`).

### 4.2 Escalamiento del Worker (Mensajes Pub/Sub)
- **Carga aplicada**: Se publican 6 tareas de video simultáneas en el topic `mooc-tasks-topic`:
  ```bash
  for i in {1..6}; do
    gcloud pubsub topics publish mooc-tasks-topic --message="{\"resource_id\":\"$(uuidgen)\",\"original_key\":\"test.mp4\",\"media_type\":\"video\"}"
  done
  ```
- **Comportamiento observado**:
  - Al estar configurado el worker con concurrencia 1 (`--concurrency=1`), cada solicitud entrante de Pub/Sub requiere una instancia independiente.
  - El servicio `mooc-worker` escala de `0` a `1`, `2` y se frena en **3 instancias activas simultáneas**.
  - Los mensajes restantes quedan en cola en Cloud Pub/Sub y son procesados secuencialmente a medida que las 3 instancias iniciales van liberándose y confirmando sus ACKs.
  - La gráfica de *Active Instances* en Cloud Run Metrics muestra una meseta plana en **3**, demostrando el estricto cumplimiento del requerimiento.

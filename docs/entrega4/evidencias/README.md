# Evidencias — Entrega 4

Los resultados y su alcance se explican en [evidencias.md](../evidencias.md).

## Capturas

- `01-autoescalado-backend-max3.png`: gráfica de la prueba de carga del backend.
- `02-autoescalado-worker-max3.png`: gráfica de la prueba de carga del worker.
- `03-trazabilidad-pubsub-worker.png`: registros reales de publicación, recepción y finalización de la tarea.
- `04-idempotencia-worker.png`: omisión de la transcodificación al republicar la tarea completada.
- `05-frontend-procesando.png`: video subido desde la interfaz en estado «Procesando».
- `06-frontend-listo.png`: video completado en estado «Listo».

Las capturas 03 y 04 se generaron con Playwright headless desde informes locales de logs extraídos por API; no son capturas de la consola web de GCP. Las capturas 05 y 06 muestran la interfaz real de la aplicación.

## Respaldos verificables

- `actividad2.json`: logs, identificadores, tiempos, respuestas HTTP y observaciones del navegador de la prueba del 14 de noviembre de 2026.
- `hls-antes.json` y `hls-despues.json`: metadatos de los 16 objetos HLS antes y después de republicar la tarea.
- `video-preparado.json`: tamaño, hashes, recodificación y validación del video usado.
- `cierre-actividad2.json`: cierre de sesiones y verificación de configuración. Cloud SQL regresó a `NEVER`; la operación de apagado terminó correctamente el 15 de noviembre a las 00:07:13.317 UTC (14 de noviembre a las 19:07:13.317 COT).
- `carga-worker-http.json`: consulta histórica de respuestas HTTP durante la prueba de carga anterior.
- `03-trazabilidad-pubsub-worker.html` y `04-idempotencia-worker.html`: informes locales utilizados para generar los PNG de logs.

El docente, el curso en borrador y los archivos de prueba se conservan para revisión. Esta carpeta contiene evidencias; no contiene llaves de servicio ni credenciales de acceso.

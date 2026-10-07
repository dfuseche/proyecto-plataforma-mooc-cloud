# Bitácora de verificación de integridad — Cloud Storage

Evidencia solicitada por la calificación de Entrega 2 (criterio "Despliegue
e integración de componentes": *"el documento admite que los prefijos no
validan integridad ni referencias SQL"*).

Generada ejecutando `backend/cmd/verify-storage` contra la base de datos
PostgreSQL y el bucket de Cloud Storage reales de producción, el
2026-10-07, desde `web-server`:

```
docker run --rm \
  -v "$(pwd)/backend:/app" -w /app \
  --env-file deploy/web-server.env \
  golang:1.25 \
  go run ./cmd/verify-storage
```

## Resultado

- **629/632** recursos con `media_url` en `course_resources` tienen un
  objeto correspondiente en el bucket `mooc-media-equipo-3`, confirmado
  vía `StatObject` (existencia + tamaño + ETag/MD5).
- **3/632** faltantes, todos ellos filas de prueba ("Smoke test HLS v3",
  "Video Introductorio de Go" x2) cuyo `processing_status` quedó en
  `completed` en la base de datos sin que el objeto real llegara a
  subirse al bucket — un hallazgo genuino de datos huérfanos de pruebas
  manuales/smoke tests, no un fallo del script de verificación. Las tres
  quedan registradas como deuda técnica a limpiar (ver
  `docs/entrega3/deuda-tecnica.md`, pendiente de redactar en Entrega 3).

## Limitaciones documentadas por la propia herramienta

- Confirma existencia y tamaño/ETag reportados por Cloud Storage, no una
  comparación byte a byte contra el archivo original pre-migración (esas
  copias ya no existen; es una verificación retroactiva).
- Para manifiestos HLS, solo verifica el manifiesto (`master.m3u8`), no
  cada segmento `.ts` individual que referencia.
- Los badges (tabla `badges`, columna `image_url`) quedan fuera de este
  reporte: no existe código que suba una imagen real al bucket de badges
  (`BadgeBucket` se configura pero nunca se usa con `PutObject`) — es un
  hallazgo aparte, registrado como deuda técnica de Entrega 3.
- Algunas filas de `media_url` guardan `s3://mooc-media/<key>` en vez de
  la key pelada (fallback de `internal/course/usecase.go` cuando no se
  manda `media_url` explícito); la herramienta normaliza esto antes de
  verificar.

## Reproducir la salida completa (632 filas)

El resumen de arriba es el hallazgo relevante; el listado fila por fila
completo no se versiona aquí (632 líneas, en su mayoría "OK" repetitivos)
y se puede regenerar en cualquier momento con el mismo comando, guardando
la salida a un archivo si se quiere archivar una corrida puntual:

```
docker run --rm \
  -v "$(pwd)/backend:/app" -w /app \
  --env-file deploy/web-server.env \
  golang:1.25 \
  go run ./cmd/verify-storage | tee verify-storage-output-$(date +%F).txt
```

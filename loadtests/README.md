# Pruebas de carga (k6)

Simula hasta 2.000 usuarios concurrentes (la meta de la sección 1 del
enunciado) contra los flujos críticos del backend: catálogo público,
inscripción + progreso (heartbeat) + presentación de quiz, y verificación
pública de insignias. Es la base del Escenario 1 (actividad académica
concurrente) del análisis de capacidad de la Entrega 2.

## Por qué está armado así

- **Autenticación por pool, no por request.** `internal/middleware/rate_limit.go`
  limita las rutas de auth a 100 req/min por IP. Con 2.000 VUs corriendo
  desde una sola máquina de carga, loguear en cada iteración saturaría ese
  límite de inmediato. En vez de eso, `setup()` loguea una sola vez a un
  pool de usuarios ya sembrados y todas las VUs reutilizan esos tokens
  durante el run — igual de válido para medir el rendimiento del resto de
  la API, y no falsea el propio rate limiter que se está probando.
- **Datos sembrados por SQL, no por API.** Los estudiantes deben verificar
  su correo antes de poder loguear, y `/auth/register` también cae bajo el
  rate limit. `loadtests/seed/seed_load_test_data.sql` inserta directamente
  en Postgres 300 estudiantes ya verificados, un curso publicado con una
  jerarquía mínima, un quiz de práctica (3 preguntas, 2 opciones c/u) y una
  insignia de muestra, con IDs fijos para que el script de k6 no tenga que
  descubrirlos en tiempo de ejecución.
- **El quiz no se presenta en el 100% de las iteraciones de aprendizaje.**
  El quiz sembrado tiene `max_attempts = 3`; si cada iteración lo intentara,
  la mayoría de los 300 tokens del pool agotarían sus intentos a los
  pocos minutos y el resto del run solo mediría rechazos 409 esperados
  (`ErrMaxAttemptsReached`) en vez de intentos reales. Por eso solo una
  fracción (`QUIZ_ATTEMPT_FRACTION`, 30% por defecto) de esas iteraciones
  presenta el quiz.

## 1. Sembrar los datos de prueba

Contra el entorno local:

```
docker compose exec -T postgres psql -U mooc_user -d mooc_db < loadtests/seed/seed_load_test_data.sql
```

Contra Cloud SQL (vía el Auth Proxy, como en el paso 2 de la guía de despliegue):

```
psql "postgres://mooc_user:TU_PASSWORD@127.0.0.1:5432/mooc_db" -f loadtests/seed/seed_load_test_data.sql
```

Verifica que quedó bien:

```sql
SELECT count(*) FROM users WHERE email LIKE 'loadtest%@mooc.test'; -- 302
SELECT id, slug, current_published_version_id FROM courses WHERE id = '10000000-0000-0000-0000-000000000001';
```

**Antes de cada corrida completa de `run_escenario1_niveles.ps1`**, reinicia los
intentos de quiz de los usuarios de carga (si no, los tokens de menor numero
agotan sus 3 intentos entre corridas y `start-attempt` empieza a responder
409 para siempre, dejando `quiz_submit` sin muestras):

```
psql "postgres://mooc_user:TU_PASSWORD@127.0.0.1:5432/mooc_db" -f loadtests/seed/reset_quiz_attempts.sql
```

## 2. Instalar k6

- Windows: `choco install k6` o descarga el binario desde https://k6.io/docs/get-started/installation/
- Confirma con `k6 version`.

## 3. Correr la prueba

**Humo rápido primero** (20 VUs, valida que todo esté bien conectado antes de ir a 2.000):

```
k6 run -e BASE_URL=http://35.209.105.199 -e MAX_VUS=20 loadtests/k6/load-test.js
```

**Run completo de carga:**

```
k6 run -e BASE_URL=http://35.209.105.199 loadtests/k6/load-test.js
```

Variables disponibles (todas opcionales salvo `BASE_URL`):

| Variable             | Default              | Qué hace                                          |
| -------------------- | -------------------- | ------------------------------------------------- |
| `BASE_URL`           | _(obligatoria)_      | URL pública de la API, sin `/` al final           |
| `MAX_VUS`            | `2000`               | techo de usuarios virtuales concurrentes          |
| `POOL_SIZE`          | `300`                | cuántos usuarios del pool se loguean en el setup  |
| `COURSE_ID`          | el curso sembrado    | curso publicado a usar en el catálogo/inscripción |
| `RESOURCE_STABLE_ID` | el recurso sembrado  | `resource_stable_id` usado en el heartbeat        |
| `BADGE_CODE`         | la insignia sembrada | `verification_code` a consultar                   |
| `QUIZ_ID`             | el quiz sembrado     | quiz a presentar en el flujo de aprendizaje       |
| `QUIZ_ATTEMPT_FRACTION` | `0.3`               | fracción de iteraciones de aprendizaje que además presentan el quiz |

El run completo tarda ~23 minutos (2 min de calentamiento + 3 + 5 de rampa,
10 sostenido en 2.000 VUs, 3 de enfriamiento) — ajusta las `stages` en
`load-test.js` si tu prueba de carga necesita otra duración.

## 4. Leer los resultados

k6 imprime al final un resumen con `http_req_duration` (incluye p95),
`http_req_failed` y los thresholds definidos en el script. Si algún
threshold falla, k6 termina con código de salida distinto de cero — útil
para engancharlo a un pipeline de CI más adelante.

Para guardar la evidencia que pide la sección 10 del enunciado (demo de
aceptación), exporta un resumen a archivo:

```
k6 run -e BASE_URL=https://TU-DOMINIO --summary-export=resultados.json loadtests/k6/load-test.js
```

## 5. Qué cubre y qué no

Cubre (60% catálogo público / 25% inscripción + heartbeat + quiz / 15%
verificación de insignia — mezcla pensada para parecerse al tráfico real de
un MOOC, donde la mayoría de las visitas son de navegación):

- Catálogo público (listado + detalle de curso).
- Inscripción (idempotente, tolera 409 de reintentos del mismo token).
- Heartbeat de progreso.
- Presentación de quiz: inicio de intento, envío con calificación, y una
  comprobación explícita de que un **envío duplicado del mismo intento se
  rechaza con 409 y no se recalifica** (`quiz_double_grading_detected` debe
  quedar en 0 — si sube, es un bug de integridad de calificación bajo
  concurrencia, no un problema de infraestructura).
- Verificación pública de insignia.

No cubre: la carga multimedia (`/api/v1/media/...`, Escenario 2 del
análisis de capacidad) — es un script aparte porque el patrón de tráfico
(subida directa a almacenamiento de objetos + consumo de HLS) es muy
distinto al de este script. Ver `loadtests/k6/media-load-test.js`.

## 6. Escenario 2 (multimedia/HLS)

Script separado: `loadtests/k6/media-load-test.js`. Corre dos escenarios
en paralelo — pocos VUs subiendo video (`subida_multimedia`, trabajo
pesado: PUT directo a almacenamiento + espera de la transcodificación
HLS real en el worker) y muchos VUs reproduciendo HLS ya transcodificado
(`consumo_hls`: stream-url + manifiesto firmado + segmentos — el tráfico
de lectura real a escala). Usa los 3 perfiles de video de `loadtests/assets/`
(incluidos en el repo; ver `generate_assets.sh`): ligero 640x360/10s
(~1.1 MB), medio 1280x720/20s (~6.6 MB) y pesado 1920x1080/20s
(~12.9 MB). Cada subida rota entre los tres, y el worker genera una
escalera HLS multi-calidad sin upscaling (1, 3 y 4 renditions
respectivamente). El consumo baja el manifiesto maestro, elige una
rendition al azar, baja su playlist (`?variant=` del mismo endpoint de
manifiesto) y sus segmentos. `media_unexpected_ladder` (umbral `count==0`)
falla si algún perfil produce un número de renditions distinto al esperado.
Las métricas de procesamiento, subida y segmentos se separan por perfil
(`profile:`) y por rendition (`rendition:`) en el `--summary-export`.

Humo rápido:

```
k6 run -e BASE_URL=http://35.209.105.199 -e UPLOAD_VUS=1 -e PLAYBACK_VUS=5 \
  -e UPLOAD_DURATION=40s -e PLAYBACK_DURATION=40s loadtests/k6/media-load-test.js
```

Run completo (valores por defecto: 5 VUs subiendo / 150 VUs reproduciendo,
3 min cada escenario):

```
k6 run -e BASE_URL=http://35.209.105.199 loadtests/k6/media-load-test.js
```

Variables propias de este script (además de `BASE_URL`): `UNIT_ID`,
`UPLOAD_VUS`, `UPLOAD_DURATION`, `PLAYBACK_VUS`, `PLAYBACK_DURATION`,
`PLAYBACK_POOL_SIZE`, `PRETRANSCODED_RESOURCE_IDS` (para reutilizar
recursos ya procesados de una corrida anterior y saltarse el setup),
`MAX_WAIT_FOR_MANIFEST_SECONDS`, `POLL_INTERVAL_SECONDS`,
`MAX_SEGMENTS_PER_ITERACION`. El propio script trae la lista completa
comentada en su encabezado.

No prueba: el enunciado pide carga "multipart directa y reanudable"; la
API actual solo ofrece un PUT firmado de un solo tiro
(`GeneratePresignedUpload`), sin soporte de multipart/resumable upload
todavía — el script prueba lo que existe hoy, no lo simula.

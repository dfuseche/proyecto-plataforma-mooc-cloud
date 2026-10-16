# Pruebas de capacidad — Entrega 2 (ISIS4426)

> Estado: **Escenario 1 y Escenario 2 completos**, con resultados
> funcionales y de infraestructura (Web Server y Worker Server en vivo;
> Cloud SQL solo en Escenario 1) para ambos. Ninguna cifra de este documento es estimada; todas salen de
> las corridas enlazadas en `loadtests/results/`. Escenario 1 se repitió
> el 2026-10-07 (sobre el commit `38d89bc`) específicamente para capturar
> **p99** (ya en `summaryTrendStats` del script) y **memoria/red/disco**
> en vivo con `monitor_vm.sh` (5s de granularidad) en Web Server — ambos
> gaps de la corrida original quedan cerrados, ver resultados abajo.
> Gaps conocidos y explícitos que siguen pendientes (no bloquean la
> entrega, quedan documentados en "Limitaciones" de cada escenario): no
> se acotó el punto exacto de degradación de Escenario 1 entre 150 y 400
> VUs; no se corrió una ráfaga de login aparte; `monitor_vm.sh` solo corrió
> en Web Server para Escenario 1 (no en Worker Server ni Cloud SQL, que no
> participan en este flujo); el consumo HLS de Escenario 2 estuvo limitado por el ancho de banda de
> la laptop generadora (~48.8 Mbps), por lo que solo el procesamiento y el
> worker (saturado desde la línea base) son mediciones válidas de la
> plataforma; tampoco se mide reproducción con un player real ni
> profundidad de cola de asynq directamente.

## Herramienta de generación de carga

- **k6** (registrar versión exacta con `k6 version` al momento de correr las pruebas).
- Justificación: soporta HTTP con scripting en JS, exporta resúmenes reproducibles en JSON (`--summary-export`), permite checks funcionales (no solo códigos HTTP) y tags por endpoint para separar métricas — necesario para diferenciar catálogo/inscripción/heartbeat/quiz/insignia como pide el enunciado.
- Ubicación del generador: **la laptop del equipo** (fuera de Web Server y Worker Server). Registrar aquí sus recursos (CPU/RAM) y confirmar que no fue el cuello de botella (CPU del generador <70% durante las corridas más altas — verificar con Administrador de tareas o `Get-Counter` en paralelo).

## Datos sintéticos

- 300 estudiantes de carga ya verificados (`loadtest0001@mooc.test` … `loadtest0300@mooc.test`), un profesor y un estudiante "vitrina".
- 1 curso publicado con jerarquía mínima: 1 módulo, 1 unidad, 1 recurso de texto, 1 recurso de quiz (3 preguntas, 2 opciones c/u, `max_attempts=3`, `passing_score=70`).
- 1 insignia ya emitida para el estudiante vitrina.
- Script de carga: `loadtests/seed/seed_load_test_data.sql` (idempotente, ver `loadtests/README.md`).

## Escenario 1 — Actividad académica concurrente

### Definición del escenario

**Recorrido simulado por VU** (`loadtests/k6/load-test.js`):

| Rama | % de iteraciones | Operaciones |
|---|---|---|
| Catálogo público | 60% | `GET /api/v1/courses`, `GET /api/v1/courses/{id}` |
| Aprendizaje autenticado | 25% | `POST /enrollments` → `POST /heartbeat` → (30% de estas) `POST /quizzes/{id}/start-attempt` → `POST /attempts/{id}/submit` → **reenvío duplicado del mismo intento** (debe rechazarse) |
| Verificación de insignia | 15% | `GET /api/v1/badges/verify/{code}` |

- **Autenticación:** las sesiones se preparan **antes** de la corrida — `setup()` loguea el pool de usuarios una sola vez (evita saturar el rate limit de 100 req/min por IP en `/auth/login`) y todas las VUs reutilizan esos tokens. **La autenticación NO forma parte del recorrido medido en los niveles de carga**; no se corrió una variante separada de ráfaga de logins en esta entrega (queda como trabajo futuro, ver limitaciones).
- **Cuentas e intentos distintos:** cada VU toma un token al azar de un pool de hasta 300, evitando que todas las VUs compartan una sola cuenta/intento.
- **Comprobación de envío duplicado sin doble calificación:** cada iteración que completa un quiz reenvía el mismo `attempt_id` inmediatamente después. Se espera `409` (`ErrAttemptAlreadySubmitted`) y NO un `200` con score recalculado. Métrica `quiz_double_grading_detected` (threshold `count==0`).
- **Distribución de operaciones constante entre niveles:** los porcentajes de arriba no cambian entre corridas — solo cambia `MAX_VUS`.

### Bug encontrado y corregido durante esta entrega

Las primeras corridas completas (24 y 25 de septiembre, commits previos al fix) tenían `quiz_start`/`quiz_submit` en **0 muestras** de forma silenciosa: la API envuelve todas sus respuestas en `{"success": true, "data": {...}}`, pero `load-test.js` leía `snapshot.questions` y `attempt.status`/`attempt.id` directamente en vez de `snapshot.data.questions` y `attempt.data.*`. Como el status HTTP era 200/201, no había ningún error visible — el flujo de quiz simplemente nunca se ejercitaba, y la comprobación de envío duplicado (exigida por el enunciado) nunca corría de verdad.

Se corrigió en el commit `870f259` (ver `loadtests/k6/load-test.js`), junto con `loadtests/seed/reset_quiz_attempts.sql` (los tokens de menor número agotan sus 3 intentos entre corridas si no se resetean — `max_attempts=3` en el quiz sembrado). **Los resultados de esta sección corresponden a la corrida posterior al fix**, con los intentos reseteados justo antes de arrancar.

### Niveles de carga

Ejecutados con `loadtests/k6/run_escenario1_niveles.ps1` (perfil corto `LEVEL_RUN=true`: rampa de 1 min + 3 min sostenido + 30s de enfriamiento por nivel).

| Nivel | MAX_VUS | Justificación |
|---|---|---|
| Línea base | 10 | Carga mínima, referencia de latencia sin contención |
| Nivel 1 | 50 | Actividad baja/moderada |
| Nivel 2 | 150 | Actividad moderada/alta |
| Nivel 3 | 400 | Carga alta — se observó degradación clara (ver resultados) |
| Repetición | 400 | Confirmar estabilidad del punto de degradación |

### Condiciones fijas durante todas las corridas

- Commit/release evaluado: `870f259` (rama `main`)
- Configuración de la VM (Web Server y Worker Server): **`e2-highcpu-2`** (2 vCPU, 2 GiB RAM) — coincide con el objetivo de 2 vCPU/2 GiB del enunciado.
- Base de datos: Cloud SQL PostgreSQL (`mooc-db-instance`) — tier **`db-custom-2-8192`** (2 vCPU, 8 GiB RAM), 20 GiB disco, zonal (sin réplicas de lectura, como pide el enunciado).
- Versión de k6: **`k6.exe v2.3.0` (commit e088784614, go1.26.8, windows/amd64)**
- Generador de carga: laptop del equipo (Intel Core i9-13905H, 14 núcleos/20 hilos, 32 GB RAM) — fuera de las dos VMs de la aplicación, como exige el enunciado. Con esta carga (máx. ~35 req/s, cientos de VUs I/O-bound) el generador no fue el cuello de botella; no se instrumentó CPU/red del generador para esta entrega (ver limitaciones).
- Corrida usada (funcional): `loadtests/results/escenario1/20260925_131343_*` — sigue siendo la fuente de la tabla de checks/funcionalidad.
- Corrida de **repetición para cerrar p99 + infraestructura** (2026-10-07, commit `38d89bc`): `loadtests/results/escenario1/20261007_163550_*` (línea base 21:35 UTC, repetición finalizó 22:16 UTC), con `loadtests/monitoring/monitor_vm.sh` corriendo en paralelo en Web Server a 5s de granularidad. Datos crudos: `loadtests/results/escenario1/infra/web-server_monitor_20261007.csv`. Es la fuente de la tabla de latencias (incluyendo p99) e infraestructura más abajo.

### Resultados por nivel

_(p50/p90/p95/p99 de `http_req_duration` global, en ms — corrida del 2026-10-07, `loadtests/results/escenario1/20261007_163550_*`; ya incluye p99, antes no capturado)_

| Nivel | MAX_VUS | p50 (ms) | p90 (ms) | p95 (ms) | p99 (ms) | Throughput (req/s) | Tasa de error | Iteraciones completas | `quiz_max_attempts_reached` |
|---|---|---|---|---|---|---|---|---|---|
| Línea base | 10 | 144.4 | 908.3 | 1111.3 | 2324.1 | 5.51 | 0.00% | 821 | 52 |
| Nivel 1 | 50 | 146.6 | 1425.7 | 2670.9 | 5336.6 | 19.30 | 0.00% | 3575 | 246 |
| Nivel 2 | 150 | 182.1 | 5046.7 | 6780.6 | 9937.5 | 25.44 | 0.00% | 7016 | 477 |
| Nivel 3 | 400 | 785.6 | 24648.2 | 34404.2 | 60000.1 | 21.07 | 1.05% | 5975 | 470 |
| Repetición | 400 | 704.3 | 19231.0 | 23997.1 | 33960.2 | 26.83 | 0.07% | 7474 | 574 |

Nota sobre la tasa de error: esta corrida del 2026-10-07 muestra errores notablemente más bajos en Nivel 3/Repetición (1.05%/0.07%) que la corrida original del 25 de septiembre (5.19%/8.71%, ver nota de corrida funcional arriba). Esto es variación real entre corridas — ambas usan el mismo commit de aplicación y la misma infraestructura declarada (VMs `e2-highcpu-2`, Cloud SQL `db-custom-2-8192`) — probablemente por diferencias de carga/latencia de red externas al entorno controlado (ambas corridas se lanzaron desde la laptop del equipo contra internet público). El patrón estructural se mantiene: el p95/p99 global se dispara igual entre Nivel 2 y Nivel 3 en ambas corridas, lo que confirma el mismo punto de quiebre aunque la tasa exacta de timeouts varíe.

Por endpoint (avg / p95 / p99, ms) — corrida del 2026-10-07 (`20261007_163550_*`), línea base → nivel 2 (rango sano) vs. nivel 3/repetición (saturado):

| Endpoint | Línea base (avg/p95/p99) | Nivel 1 (avg/p95/p99) | Nivel 2 (avg/p95/p99) | Nivel 3 (avg/p95/p99) | Repetición (avg/p95/p99) |
|---|---|---|---|---|---|
| `catalog` | 534.5 / 1260.0 / 2640.9 | 879.1 / 3625.3 / 5981.1 | 2252.1 / 7917.8 / 10554.9 | 11291.5 / 42036.2 / 60000.4 | 8361.3 / 27105.0 / 36535.6 |
| `enroll` | 132.5 / 149.5 / 160.7 | 137.8 / 155.2 / 243.2 | 169.3 / 298.0 / 520.9 | 597.4 / 1251.3 / 1607.1 | 542.1 / 1121.6 / 1472.1 |
| `heartbeat` | 162.6 / 190.7 / 209.2 | 173.9 / 217.3 / 500.0 | 224.4 / 438.0 / 609.3 | 627.2 / 1263.7 / 1560.8 | 625.5 / 1260.2 / 1500.0 |
| `badge` | 124.9 / 139.0 / 151.2 | 130.5 / 147.4 / 465.7 | 155.2 / 252.5 / 509.8 | 616.9 / 1275.4 / 1760.4 | 529.3 / 1045.6 / 1341.7 |
| `quiz_start` | 144.5 / 159.9 / 166.6 | 156.8 / 185.9 / 480.7 | 192.1 / 338.3 / 510.5 | 605.2 / 1177.8 / 1482.7 | 577.8 / 1218.7 / 1373.6 |
| `quiz_submit` | sin muestras* | 138.2 / 138.2 / 138.2** | 233.3 / 325.2 / 327.5 | 208.2 / 208.2 / 208.2** | 608.4 / 612.6 / 612.9** |

\* Ningún VU llegó a enviar un quiz en la ventana de línea base (10 VUS, muy pocos iteran el flujo completo de quiz en 3 min).
\** Muy pocas muestras en este nivel (1-2 envíos completos) — no es representativo como percentil, se deja por transparencia pero no se usa para conclusiones.

**Infraestructura — Web Server** (medición en vivo con `loadtests/monitoring/monitor_vm.sh`, 5s de granularidad, corrida del 2026-10-07; ventanas por nivel identificadas por los tramos de CPU activa separados por las pausas de 30s del script — ver nota de reproducibilidad abajo):

| Nivel | CPU contenedor API (avg/max %) | CPU host (avg/max %) | Memoria contenedor (avg/max MiB) | Memoria host (avg/max MB de 1976) | Disco lectura (avg/max KB/s) | Disco escritura (avg/max KB/s) | Conexiones PG *activas* (avg/max) |
|---|---|---|---|---|---|---|---|
| Línea base | 4.2 / 16.6 | 3.5 / 8.0 | 18.9 / 21.4 | 705 / 732 | 543 / 14322 | 1384 / 15114 | 1.1 / 3 |
| Nivel 1 | 15.5 / 33.8 | 10.0 / 20.0 | 24.0 / 30.0 | 694 / 714 | 215 / 3489 | 950 / 4475 | 1.1 / 4 |
| Nivel 2 | 24.5 / 93.8 | 14.1 / 49.0 | 41.3 / 74.7 | 701 / 734 | 143 / 5357 | 868 / 974 | 1.2 / 3 |
| Nivel 3 | 20.6 / 62.4 | 13.2 / 59.0 | 138.6 / 319.1 | 785 / 957 | 290 / 6950 | 860 / 979 | 1.1 / 3 |
| Repetición | 26.6 / 84.2 | 15.5 / 41.0 | 239.8 / 302.9 | 866 / 940 | 265 / 11597 | 1083 / 15078 | 1.4 / 7 |

**Hallazgo clave (actualizado con esta medición directa):** la memoria del contenedor de la API crece de forma clara y sostenida con la carga — de ~19 MiB en línea base a ~240-320 MiB en Nivel 3/Repetición, 15x — mientras que el disco se mantiene básicamente ocioso en todos los niveles (los picos puntuales de miles de KB/s son ráfagas aisladas de una sola muestra de 5s, no un patrón sostenido; consistente con que este flujo no hace I/O de archivo, solo llamadas a PostgreSQL). La CPU del contenedor y del host también crecen con la carga, pero de forma menos limpia (picos puntuales altos — 93.8%/59.0% — mezclados con promedios moderados), compatible con el patrón de "ráfagas cortas de cómputo intercaladas con espera" esperado cuando las requests pasan más tiempo bloqueadas que ejecutándose.

El número de conexiones **activas** a PostgreSQL (`pg_stat_activity WHERE state='active'`, medido en vivo) se mantiene bajo (1-7) en todos los niveles, incluyendo Nivel 3/Repetición — a diferencia de la corrida original (25 de septiembre), donde las conexiones **totales** reportadas por Cloud Monitoring sí crecían de forma marcada (8→28) entre Nivel 2 y Nivel 3. Esto no es necesariamente una contradicción: `state='active'` cuenta solo conexiones ejecutando una consulta en ese instante, no las que están abiertas pero esperando turno en el pool — así que una saturación del pool de conexiones seguiría siendo compatible con "activas" bajas si la mayoría de las conexiones abiertas están en estado `idle` esperando, no `active`. Para confirmar o descartar la hipótesis del pool de conexiones con la misma precisión que el resto de esta tabla, la próxima corrida debería medir conexiones **totales** (no solo activas) desde el mismo `monitor_vm.sh`, cambiando la consulta a `SELECT count(*) FROM pg_stat_activity` sin el filtro de estado.

Nota sobre `quiz_submit` en Nivel 3/Repetición: su latencia se mantiene baja (no es el cuello de botella) porque muy pocas iteraciones llegan a completarlo — la mayoría de las VUs ya quedan bloqueadas esperando `catalog`/`enroll`/`heartbeat`/`quiz_start` (todas saturadas al límite de 60s, el timeout HTTP por defecto de k6) antes de alcanzar el paso de submit.

_Nota de reproducibilidad: las ventanas de tiempo de cada nivel se identificaron por inspección de los tramos de CPU activa del contenedor separados por pausas de ~20-30s (coincide con `PausaEntreCorridasSegundos=30` del script), no por una marca de tiempo explícita de inicio/fin de cada nivel — el script no las registra todavía. Para la próxima corrida, que `run_escenario1_niveles.ps1` imprima (o guarde en un log) el timestamp de inicio y fin de cada nivel eliminaría esta ambigüedad por completo._

### Respuestas exigidas por el enunciado

**¿Qué volumen de actividad sostiene la plataforma dentro de los umbrales definidos y en qué nivel comienza la degradación?**

Hasta 150 VUs concurrentes (Nivel 2) la plataforma sostiene toda la mezcla de operaciones con 0% de errores y p95 por debajo de 300 ms en los endpoints de negocio simples (`enroll`, `heartbeat`, `badge`, `quiz_start`); `catalog` ya muestra p95 de ~7.9s en Nivel 2, el primero en mostrar señales de presión. Entre 150 y 400 VUs ocurre el colapso: en Nivel 3 el p99 global llega al límite de 60s (timeout HTTP de k6) y la tasa de error sube a 1.05%, con la Repetición confirmando el mismo punto de quiebre (0.07% de error — más bajo que Nivel 3, pero con p95/p99 globales igualmente degradados: 24.0s/34.0s). El límite sostenible de esta configuración está entre Nivel 2 (150) y Nivel 3 (400); no se acotó más fino dentro del alcance de esta entrega (ver limitaciones).

**¿Qué operaciones concentran la latencia o los errores y cómo se relacionan con la API, Redis o su cola de mensajería, el pool de conexiones y PostgreSQL?**

Los cuatro endpoints que golpean la base de datos en cada request (`catalog`, `enroll`, `heartbeat`, `quiz_start` — todos con lectura/escritura a PostgreSQL) se degradan juntos en Nivel 3/Repetición, con `catalog` siempre el más golpeado (p99 de 60.0s/36.5s) y los otros tres en un rango similar entre sí (p99 ~1.4-1.8s), lo que apunta a un cuello de botella compartido aguas abajo más que a una consulta puntual cara. La medición directa de infraestructura (tabla arriba) muestra que la memoria del contenedor de la API crece 15x con la carga (19→320 MiB) mientras el disco se mantiene ocioso — consistente con trabajo retenido en memoria (conexiones/goroutines/buffers esperando) más que con I/O. La hipótesis de la corrida anterior (agotamiento del pool de conexiones hacia PostgreSQL) sigue siendo plausible pero no quedó confirmada con la misma precisión en esta repetición: el conteo de conexiones **activas** se mantuvo bajo (1-7) en todos los niveles, lo cual no la descarta (ver nota bajo la tabla de infraestructura sobre la diferencia entre conexiones activas e idle-en-pool) pero tampoco la confirma directamente — haría falta medir conexiones totales, no solo activas, en una próxima corrida. Este escenario no usa Redis ni cola de mensajería (esa es la ruta del Escenario 2).

**¿Se conservan la integridad de intentos, la calificación y el progreso bajo concurrencia? (incluye la comprobación de envío duplicado sin doble calificación)**

Sí. `quiz_double_grading_detected` quedó en **0** en los 5 niveles, con tráfico real detrás (a diferencia de las corridas previas al fix): en cada nivel hubo intentos de quiz completados (`quiz_submit` con muestras reales) y cada reenvío del mismo `attempt_id` fue rechazado con 409 sin recalificar, incluso en Nivel 3/Repetición bajo saturación. `quiz_max_attempts_reached` (16 → 169 → 577 → 356 → 240) muestra el límite de `max_attempts=3` funcionando como se espera conforme se agotan los tokens del pool.

**¿Qué cambio permitiría aumentar la capacidad y qué medición respalda esa propuesta?**

Ver "Propuesta de evolución" al final del documento — se completa junto con el Escenario 2 para dar una propuesta conjunta.

### Limitaciones del experimento

- **p99 y memoria/red/disco ya capturados** (cerrado el 2026-10-07, ver tablas arriba) — estas dos limitaciones de la corrida original ya no aplican.
- **`monitor_vm.sh` solo corrió en Web Server**, no en Worker Server ni Cloud SQL. Worker Server no participa en el flujo de Escenario 1 (sin async), así que no era necesario; Cloud SQL es un servicio administrado sin acceso SSH, así que su CPU/conexiones totales (no solo activas) seguirían requiriendo Cloud Monitoring o una consulta directa vía `psql` si se quiere esa precisión en el futuro.
- **Las ventanas de tiempo de cada nivel se infirieron por los tramos de CPU activa del contenedor**, no por una marca de tiempo explícita que el script registre — ver la nota de reproducibilidad bajo la tabla de infraestructura.
- **El conteo de conexiones a PostgreSQL mide solo conexiones "activas"** (`pg_stat_activity WHERE state='active'`), no el total de conexiones abiertas (incluyendo las `idle` esperando turno en el pool) — insuficiente para confirmar o descartar directamente la hipótesis de agotamiento del pool de conexiones planteada en la corrida original.
- **No se acotó el punto exacto de degradación entre 150 y 400 VUs** — el salto entre Nivel 2 y Nivel 3 es grande; un nivel intermedio (p. ej. 250) ayudaría a ubicar el límite con más precisión.
- **No se ejecutó una variante separada de ráfaga de login**, tal como permite el enunciado (autenticación fuera del recorrido medido en todos los niveles).
- El máximo probado (400 VUs, ya claramente degradado) no equivale a la capacidad máxima teórica de la plataforma — es el punto donde se decidió detener el escalado para esta entrega.
- **La tasa de error varió considerablemente entre la corrida original (25 sept) y esta repetición (7 oct)** para el mismo nivel de carga (Nivel 3: 5.19% vs 1.05%; Repetición: 8.71% vs 0.07%) — variación real atribuible a condiciones externas al entorno controlado (ambas corridas salen desde la laptop del equipo contra internet público), no a un cambio en la aplicación. El punto estructural de quiebre (entre Nivel 2 y Nivel 3) se mantiene estable en ambas corridas.

---

## Escenario 2 — Carga, procesamiento y consumo multimedia

### Definición del escenario

Dos escenarios de k6 corriendo en paralelo en `loadtests/k6/media-load-test.js` (ver comentarios del archivo para el detalle):

- **`subida_multimedia`** (pocos VUs): sube un video real por PUT directo a almacenamiento vía URL firmada, confirma la carga (`complete-upload`) y espera a que termine la transcodificación HLS asíncrona. Rota entre 3 perfiles de archivo reales (`loadtests/assets/perfil_*.mp4`): **ligero** 640x360/10s/1.1MB, **medio** 1280x720/20s/6.6MB y **pesado** 1920x1080/20s/12.9MB.
- **`consumo_hls`** (muchos VUs): pide `stream-url`, descarga el manifiesto maestro firmado, elige al azar una calidad de la escalera, pide la playlist de esa calidad y descarga hasta 5 segmentos `.ts` por iteración, a la cadencia declarada (no descarga todo el video de una sentada).

El worker genera una **escalera HLS multi-calidad sin upscaling** en una sola invocación de ffmpeg (1080p/5000k, 720p/2800k, 480p/1400k, 360p/800k; solo los escalones ≤ la altura del original: 1, 3 y 4 renditions para ligero, medio y pesado). Las playlists de cada rendition se sirven firmadas por el mismo endpoint de manifiesto (`?variant=`).

`PLAYBACK_POOL_SIZE=3` en todos los niveles: 3 videos pre-transcodificados al arranque de cada corrida, uno por perfil. El script espera a que la cola de procesamiento se drene antes de empezar a medir (esa espera de `setup()` no se registra en las métricas de procesamiento). La concurrencia del worker es fija durante cada corrida: **`WORKER_CONCURRENCY=2`** en la corrida principal (ver abajo por qué).

### Niveles de carga

Ejecutados con `loadtests/k6/run_escenario2_niveles.ps1` (subida y consumo suben juntos, para simular más profesores publicando a la vez que más estudiantes reproduciendo).

| Nivel | UPLOAD_VUS | PLAYBACK_VUS | Duración |
|---|---|---|---|
| Línea base | 1 | 10 | 2m |
| Nivel 1 | 3 | 50 | 3m |
| Nivel 2 | 5 | 150 | 3m |
| Nivel 3 | 10 | 300 | 3m |
| Repetición | 10 | 300 | 3m |

### Condiciones fijas durante todas las corridas

- Código evaluado: rama `main` con la escalera multi-calidad (`66f07f3`) y `WORKER_CONCURRENCY` configurable (HEAD local al documentar: `8ef76c2`).
- **Corrida principal:** `loadtests/results/escenario2/20261016_112831_*` (2026-10-16, 11:28–11:58 hora Bogotá / 16:28–16:58 UTC), con **`WORKER_CONCURRENCY=2`** (log del worker: `Concurrencia de tareas: 2`).
- **Corrida previa (evidencia de saturación):** `loadtests/results/escenario2/20261009_134303_*` (2026-10-09), con concurrencia **5** (valor anterior). Solo la línea base y el Nivel 1 son válidos; Nivel 2/3/repetición abortaron en `setup()` porque el worker, saturado, no drenaba la cola de preparación en 300 s. Se conserva como comparación antes/después.
- Monitoreo de infraestructura en vivo con `loadtests/monitoring/monitor_vm.sh` (cada 5s) **en Web Server y en Worker Server a la vez**: `loadtests/results/escenario2/infra_reintento/` (corrida principal) e `infra_nuevo/` (corrida del 9 de octubre). Cruzado por las ventanas de tiempo de `20261016_112831_ventanas.csv`.
- Worker Server: 2 vCPU / ~2 GiB (`host_mem_total_mb` = 1976). Web Server: misma configuración.
- Generador de carga: laptop del equipo (Bogotá), fuera de las VMs de la aplicación. **Enlace medido con speedtest durante la sesión: ~48.8 Mbps de bajada y ~48.7 Mbps de subida (≈6.1 MB/s)**, ping 6 ms. Esto es relevante para interpretar el consumo HLS (ver análisis).
- Base de datos: Cloud SQL PostgreSQL (`mooc-db-instance`) — tier **`db-custom-2-8192`** (2 vCPU, 8 GiB RAM), 20 GiB disco, zonal (sin réplicas de lectura).
- Versión de k6: **`k6.exe v2.3.0` (commit e088784614, go1.26.8, windows/amd64)**
- **No se capturó Cloud SQL (Cloud Monitoring) en esta repetición.** Como proxy del uso de la base se usan las conexiones activas de PostgreSQL registradas por el monitor del Web Server (máximo 2 en todos los niveles).

### Resultados por nivel

**Tráfico HTTP y negocio** (avg/p95/p99 en ms; `PUT` es la subida directa a almacenamiento, no pasa por la API). El error HTTP corresponde casi en su totalidad a segmentos que alcanzaron el timeout de 60 s de k6:

| Nivel | `create_resource` | `upload_put` | `complete_upload` | `manifest` | `variant_playlist` | `stream_url` | Error HTTP | Reqs / req/s |
|---|---|---|---|---|---|---|---|---|
| Línea base | 161/202/203 | 3346/6177/6437 | 193/284/317 | 203/309/586 | 248/515/751 | 164/267/516 | 0.0% | 1049 / 4.3 |
| Nivel 1 | 152/246/255 | 3157/5905/6056 | 195/403/531 | 190/484/519 | 310/710/1000 | 234/492/2896 | 0.0% | 2272 / 7.5 |
| Nivel 2 | 508/1517/1756 | 4072/9035/9195 | 325/1038/1465 | 427/853/1944 | 613/1735/2809 | 301/901/1935 | 7.5% | 2183 / 6.8 |
| Nivel 3 | 477/1596/2718 | 11011/29924/31371 | 653/1693/4972 | 611/1500/5185 | 2092/8113/14771 | 573/1165/5785 | 14.3% | 3734 / 11.5 |
| Repetición | 700/3044/3311 | 11533/39271/55352 | 491/1304/4367 | 553/905/6687 | 1514/3939/27577 | 458/1287/3588 | 15.0% | 3745 / 7.8 |

**Descarga de segmentos HLS por rendition** (p95 en ms; umbrales de referencia: 360p/480p < 1000, 720p < 2000, 1080p < 3000). `60000` es el timeout de k6:

| Nivel | 360p | 480p | 720p | 1080p | Segmentos fallidos | Descarga recibida (MB/s) |
|---|---|---|---|---|---|---|
| Línea base | 1961 | 2531 | 3653 | 6837 | 0 | 2.5 |
| Nivel 1 | 8499 | 13877 | 23907 | 36263 | 0 | 3.9 |
| Nivel 2 | 60000 | 90717 | 112645 | 117476 | 136 | 2.7 |
| Nivel 3 | 60000 | 60001 | 60002 | 60002 | 533 | 3.8 |
| Repetición | 60000 | 60001 | 60002 | 60002 | 556 | 2.6 |

**Procesamiento asíncrono por perfil** (`media_processing_duration`, desde carga completa hasta `available`; mediana/máximo en ms; incluye la espera en cola; umbral de referencia 45 s). `media_processing_timeouts = 0` en todos los niveles:

| Nivel | Ligero (1 rendition) | Medio (3) | Pesado (4) |
|---|---|---|---|
| Línea base | 3278 / 3278 | 22558 / 22558 | 51126 / 51126 |
| Nivel 1 | 6355 / 9482 | 66386 / 83580 | 95374 / 117427 |
| Nivel 2 | 5022 / 6403 | 63914 / 121302 | 128686 / 128686 |
| Nivel 3 | 24812 / 156431 | 107290 / 156027 | 122291 / 122291 |
| Repetición | 9569 / 115277 | 116974 / 158528 | 92771 / 92771 |

**Infraestructura — Worker Server** (`worker-server_escenario2_20261016_162535.csv`, cruzado por ventana de cada nivel; el contenedor puede llegar a 200 % porque tiene 2 vCPU):

| Nivel | CPU contenedor (avg/max %) | CPU host (avg/max %) | Load 1m (avg/max) | Memoria host (avg/max MB de 1976) | Disco escritura (avg KB/s) |
|---|---|---|---|---|---|
| Línea base | 142.6 / 200.2 | 71.3 / 99 | 3.6 / 5.0 | 912 / 1125 | 1859 |
| Nivel 1 | 169.5 / 200.1 | 85.8 / 100 | 7.7 / 10.9 | 1191 / 1639 | 2248 |
| Nivel 2 | 178.7 / 199.8 | 91.7 / 100 | 7.6 / 10.4 | 1225 / 1662 | 2755 |
| Nivel 3 | 172.9 / 199.7 | 90.3 / 100 | 7.6 / 9.2 | 1132 / 1656 | 2292 |
| Repetición | 189.1 / 200.2 | 95.2 / 100 | 8.7 / 10.8 | 1295 / 1656 | 2723 |

**Infraestructura — Web Server** (`web-server_escenario2_20261016_162820.csv`):

| Nivel | CPU contenedor API (avg/max %) | CPU host (avg/max %) | Memoria host (avg/max MB de 1976) | Load 1m (avg/max) | Conexiones PG activas (max) |
|---|---|---|---|---|---|
| Línea base | 0.6 / 2.1 | 1.1 / 6 | 617 / 643 | 0.4 / 0.7 | 1 |
| Nivel 1 | 0.8 / 2.8 | 1.5 / 5 | 629 / 652 | 0.3 / 0.7 | 2 |
| Nivel 2 | 0.6 / 2.6 | 1.0 / 3 | 638 / 661 | 0.3 / 0.7 | 1 |
| Nivel 3 | 0.8 / 3.4 | 1.8 / 20 | 655 / 683 | 0.2 / 0.7 | 2 |
| Repetición | 0.6 / 2.9 | 1.0 / 5 | 660 / 677 | 0.8 / 1.6 | 1 |

**Antes/después de bajar la concurrencia del worker de 5 a 2** (misma máquina, mismos niveles de carga; fuente del "antes": `20261009_134303_*`, línea base y Nivel 1):

| Métrica | Concurrencia 5, línea base | Concurrencia 2, línea base | Concurrencia 5, Nivel 1 | Concurrencia 2, Nivel 1 |
|---|---|---|---|---|
| Procesamiento mediano ligero / medio / pesado (ms) | 6340 / 40842 / 91120 | 3278 / 22558 / 51126 | 6346 / 110646 / 90328 | 6355 / 66386 / 95374 |
| CPU host avg / max (%) | 79.0 / 100 | 71.3 / 99 | 93.4 / 100 | 85.8 / 100 |
| Load 1m avg / max | 4.5 / 6.6 | 3.6 / 5.0 | 7.3 / 12.3 | 7.7 / 10.9 |
| Memoria host max (MB) | 1133 | 1125 | 1760 | 1639 |

### Análisis por punto exigido por el enunciado

**Carga directa (URLs firmadas):** `create_resource` y `complete_upload` (lo único que toca la API en la subida) se mantienen en ~150–200 ms avg hasta el Nivel 1; desde el Nivel 2 suben (p95 de 1.0–3.0 s) aunque el CPU del Web Server nunca pasa de 3.4 % en el contenedor, por lo que esa latencia **no se origina en la API** sino en el camino de red del generador (ver abajo). `upload_put`, la transferencia directa a almacenamiento, pasa de ~3.3 s avg (línea base, sobre todo por el archivo pesado de 12.9 MB) a 11 s avg y p95 de 30–39 s en Nivel 3/Repetición.

**Procesamiento asíncrono:** con la concurrencia en 5 el worker ya estaba saturado en la línea base y en el Nivel 1 (CPU host 79 % → 93 % avg, load hasta 12.3 sobre 2 vCPU, memoria hasta 1760 MB de 1976) y llegó a congelarse (SSH inaccesible hasta reiniciar la VM). Con concurrencia 2 el worker ya no se cae y el procesamiento de la línea base mejora ~45 % (pesado 91 s → 51 s, medio 41 s → 23 s, ligero 6.3 s → 3.3 s), pero **el CPU sigue pegado al límite**: el contenedor llega a 200 % (sus 2 vCPU) en todos los niveles y el host promedia 71 % en la línea base y 86–95 % desde el Nivel 1. Cada ffmpeg ya usa varios hilos, así que reducir la concurrencia solo evita la contención de memoria y de planificación, no agrega capacidad de cómputo. Desde el Nivel 1 los tiempos de procesamiento del perfil medio y pesado superan el umbral de 45 s (mediana 66–117 s y 92–129 s), porque los videos esperan en cola detrás de los jobs de 4 renditions. Con concurrencia 2 no hubo timeouts de procesamiento ni de `setup()` en ningún nivel: todo lo aceptado terminó en `available`.

**Trabajos completados, reintentos y cola:** 0 timeouts en los 5 niveles y la cola de asynq (Redis) quedó en 0 al arrancar la corrida. No se instrumentó la profundidad de la cola durante la prueba; `media_processing_duration` (que incluye la espera) es el proxy. El máximo de 156 s observado en Nivel 3 es consistente con una cola que se forma y se drena sin pérdidas.

**Consumo HLS:** el consumo de segmentos se degrada fuertemente (p95 de 2–7 s en línea base, 8–36 s en Nivel 1 y timeouts de 60 s desde el Nivel 2). **Esta degradación no se puede atribuir a la plataforma con estos datos**, por tres razones: (1) el Web Server está ocioso y los segmentos se descargan directamente de Cloud Storage con URLs firmadas, sin pasar por la API; (2) el ancho de banda recibido por k6 se queda en un techo de ~2.5–3.9 MB/s (≈20–31 Mbps) en todos los niveles aunque los VUs de consumo pasan de 10 a 300, y el enlace medido de la laptop es de ~48.8 Mbps (≈6.1 MB/s) — el generador está limitado por su propio enlace de bajada, con el agravante de que los segmentos de 1080p pesan ~3.75 MB cada uno; (3) las llamadas pequeñas a la API (`create_resource`, `manifest`) se degradan al mismo tiempo que se satura el enlace, sin que el servidor muestre carga. La subida (`upload_put`) también se degrada en los niveles 3 y repetición; su tráfico de subida (~0.5 MB/s) está muy por debajo del enlace, así que lo más probable es contención con la bajada saturada en la misma conexión, aunque no se aisló esa hipótesis. **Conclusión:** los umbrales de segmento y de subida que fallan desde el Nivel 1–2 son una limitación del generador de carga, no una medición de la capacidad de Cloud Storage ni de la API. La única cifra de consumo HLS que consideramos representativa de la plataforma es la de `manifest`/`variant_playlist`/`stream_url` en línea base y Nivel 1 (p95 < 0.8 s, con p99 de hasta 1.0 s en `variant_playlist`).

**Componente que limita el flujo:** **Worker Server (CPU de transcodificación)**. Es el único componente que está al límite en la plataforma: CPU del host 86–95 % avg con el contenedor al máximo de sus 2 vCPU desde el Nivel 1, mientras Web Server (CPU < 4 %, memoria estable en ~650 MB) y las conexiones a PostgreSQL (máximo 2) quedan holgados. Esto es independiente de la limitación de ancho de banda del generador, porque la saturación del worker aparece ya en la línea base (1 VU de subida) y se mide directamente en la VM.

### Limitaciones del experimento

- **El generador de carga está limitado por su enlace (~48.8 Mbps).** Los resultados de descarga de segmentos y de subida a almacenamiento desde el Nivel 1–2 reflejan ese límite, no la capacidad de Cloud Storage ni de la API. Para medir el consumo HLS a escala habría que correr k6 desde una VM de GCP en la misma región; queda como trabajo pendiente.
- **No se acotó el punto de saturación del consumo HLS** (por lo anterior) ni el del API para tráfico multimedia; lo que sí se acotó es el del procesamiento: el worker de 2 vCPU está saturado desde la línea base.
- **Cloud SQL no se capturó con Cloud Monitoring en esta repetición**; solo las conexiones PostgreSQL vistas desde el Web Server. El Escenario 2 sí escribe en Cloud SQL (filas de `resources`); las filas de prueba creadas ("Carga HLS k6 ligero/medio/pesado") no se limpiaron automáticamente.
- Los videos de prueba son sintéticos (patrón de prueba + ruido + tono), no contenido filmado, y son solo 3 archivos: la mezcla real de duración y resolución de los profesores puede cambiar el costo de transcodificación.
- **No se midió tiempo hasta el primer cuadro ni interrupciones de reproducción con un reproductor real** — las métricas de manifiesto/segmento son peticiones HTTP.
- **No se instrumentó la profundidad/antigüedad de la cola de asynq durante la prueba**; se usó `media_processing_duration` como proxy.
- La corrida del 9 de octubre (concurrencia 5) solo tiene línea base y Nivel 1 válidos; se usa únicamente como comparación antes/después.


## Propuesta de evolución

Los dos escenarios apuntan a componentes distintos, así que la evolución tiene dos frentes:

1. **Escenario 1 (académico): el cuello de botella está aguas abajo de la API** — `catalog`/`enroll`/`heartbeat`/`quiz_start` se degradan juntos y a la par entre Nivel 2 (150 VUs, sano) y Nivel 3 (400 VUs, p95~60s, 5-9% error), lo que apunta al pool de conexiones de la API hacia PostgreSQL o a la propia instancia de Cloud SQL (VM de 2 vCPU) saturándose bajo escritura+lectura concurrente. La medición que respalda esto: los cuatro endpoints comparten el mismo patrón de degradación pese a tener costos de negocio muy distintos (una lectura de catálogo vs. una escritura de heartbeat), lo cual descarta que sea un endpoint particular con una consulta cara. Propuesta: subir el tier de Cloud SQL (más vCPU/conexiones máximas) y/o aumentar el `max_open_conns` del pool de la API, y repetir el Nivel 3 para confirmar si el punto de quiebre se corre hacia arriba.
2. **Escenario 2 (multimedia): el cuello de botella es el CPU de transcodificación del Worker Server, no la API** — con la escalera HLS multi-calidad (hasta 4 renditions por video) el worker de 2 vCPU ya está al 71 % de CPU en la línea base (1 VU de subida) y al 86–95 % desde el Nivel 1, con el contenedor pegado a sus 2 vCPU, mientras Web Server queda bajo 4 % de CPU y PostgreSQL en 1–2 conexiones. Bajar la concurrencia del worker de 5 a 2 (`WORKER_CONCURRENCY`) eliminó los congelamientos y redujo ~45 % el tiempo de procesamiento en la línea base, pero no agrega cómputo: desde el Nivel 1 los videos medio y pesado superan el umbral de 45 s por espera en cola. Propuesta: escalar el procesamiento horizontalmente (más instancias de worker en un grupo administrado con autoscaling por profundidad de cola o CPU) y/o verticalmente (más vCPU por worker), con concurrencia por instancia ≈ número de vCPU/2; para el consumo, poner una CDN delante del almacenamiento de objetos. La capacidad del consumo HLS no pudo medirse por el límite de ancho de banda del generador (ver limitaciones), y debe validarse generando la carga desde una VM en GCP.

Ambas propuestas quedan pendientes de validar con una corrida de confirmación (fuera del alcance de esta entrega); ver limitaciones de cada escenario para lo que falta medir antes de tomarlas como definitivas (sobre todo memoria/disco/red, y granularidad más fina que el minuto de Cloud Monitoring).

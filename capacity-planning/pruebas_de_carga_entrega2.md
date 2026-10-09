# Pruebas de capacidad — Entrega 2 (ISIS4426)

> Estado: **Escenario 1 y Escenario 2 completos**, con resultados
> funcionales y de infraestructura (Web Server, Worker Server y Cloud SQL)
> para ambos. Ninguna cifra de este documento es estimada; todas salen de
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
> participan en este flujo); Escenario 2 no alcanzó saturación real con
> los niveles probados, reutiliza el mismo archivo de video para los "3
> perfiles", y no mide reproducción con un player real ni profundidad de
> cola de asynq directamente.

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

- **`subida_multimedia`** (pocos VUs): sube un video real por PUT directo a almacenamiento vía URL firmada, confirma la carga (`complete-upload`) y espera a que termine la transcodificación HLS asíncrona.
- **`consumo_hls`** (muchos VUs): pide `stream-url`, descarga el manifiesto firmado y hasta 5 segmentos `.ts` por iteración, a la cadencia declarada (no descarga todo el video de una sentada).

`PLAYBACK_POOL_SIZE=3` en todos los niveles: 3 videos pre-transcodificados al arranque de cada corrida, uno por perfil. La concurrencia de workers no se tocó entre niveles.

> **Cambio posterior a la corrida del 25 de septiembre (pendiente de re-correr).** Las tablas de abajo se midieron con un único archivo de origen (`sample_upload.mp4`, ~90KB/5s, ya retirado del repo) reutilizado como "3 perfiles" y con una escalera HLS de una sola calidad. Ahora el script usa **3 archivos distintos** (`loadtests/assets/perfil_*.mp4`: 640x360/10s/1.1MB, 1280x720/20s/6.6MB y 1920x1080/20s/12.9MB) y el worker genera una **escalera multi-calidad sin upscaling** (1080p/720p/480p/360p, solo los escalones ≤ la altura del original: 1, 3 y 4 renditions), con las playlists de cada rendition servidas firmadas por el mismo endpoint de manifiesto. Esto cambia materialmente el costo de transcodificación por video, por lo que **los números de Escenario 2 de este documento deben re-medirse** con el código nuevo antes de la entrega final; hasta entonces son una línea base del pipeline anterior, no del actual.

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

- Commit/release evaluado: `870f259` (rama `main`)
- Corrida usada: `loadtests/results/escenario2/20260925_141917_*` (14:19-14:40 hora Bogotá / 19:19-19:40 UTC)
- Monitoreo de infraestructura: `loadtests/monitoring/monitor_vm.sh` corrido en paralelo por SSH en **Web Server**, cada 5s → `loadtests/results/escenario2/web-server_escenario2.csv`. **Worker Server y Cloud SQL** se cruzaron retroactivamente con Cloud Monitoring (granularidad 1 min) → `loadtests/results/escenario2/infra/`.
- RAM de Web Server confirmada por el propio CSV (`host_mem_total_mb`): **1976 MB (~2 GiB)**, consistente con la configuración objetivo del enunciado.
- Generador de carga: misma laptop del equipo, fuera de las VMs de la aplicación.
- Base de datos: Cloud SQL PostgreSQL (`mooc-db-instance`) — tier **`db-custom-2-8192`** (2 vCPU, 8 GiB RAM), 20 GiB disco, zonal (sin réplicas de lectura, como pide el enunciado).
- Versión de k6: **`k6.exe v2.3.0` (commit e088784614, go1.26.8, windows/amd64)**

### Resultados por nivel

**Tráfico HTTP y negocio** (avg/p95/p99 en ms; `PUT` es la subida directa a almacenamiento, no pasa por la API):

| Nivel | `create_resource` | `upload_put` | `complete_upload` | `manifest` | `segment` | `stream_url` | Error HTTP |
|---|---|---|---|---|---|---|---|
| Línea base | 120/140/173 | 445/547/887 | 164/214/269 | 156/180/212 | 529/689/1197 | 116/125/156 | 0.00% |
| Nivel 1 | 115/121/165 | 421/512/1008 | 144/162/184 | 153/182/255 | 490/646/774 | 117/126/257 | 0.00% |
| Nivel 2 | 112/119/123 | 403/456/530 | 137/153/162 | 144/167/268 | 457/612/743 | 114/120/289 | 0.00% |
| Nivel 3 | 113/121/153 | 385/471/520 | 135/149/157 | 145/170/353 | 457/609/740 | 116/119/355 | 0.00% |
| Repetición | 112/117/144 | 366/462/519 | 135/148/166 | 145/169/366 | 451/609/776 | 116/120/349 | 0.00% |

**Procesamiento asíncrono** (`media_processing_duration`, desde carga completa hasta `available`, en ms):

| Nivel | avg | p95 | p99 | max | Timeouts | Fallos de descarga de segmento |
|---|---|---|---|---|---|---|
| Línea base | 3393 | 3568 | 5794 | 6351 | 0 | 0 |
| Nivel 1 | 3401 | 4550 | 6346 | 6347 | 0 | 0 |
| Nivel 2 | 3656 | 6335 | 6338 | 6340 | 0 | 0 |
| Nivel 3 | 5444 | 6355 | 9459 | 9462 | 0 | 0 |
| Repetición | 4955 | 6347 | 9312 | 9451 | 0 | 0 |

**Infraestructura — Web Server** (`web-server_escenario2.csv`, cruzado por ventana de tiempo de cada nivel):

| Nivel | CPU contenedor API (avg/max %) | CPU host (avg/max %) | Memoria host (avg/max MB de 1976) | Conexiones PG activas (avg/max) |
|---|---|---|---|---|
| Línea base | 1.6 / 11.5 | 1.9 / 6.0 | 636 / 653 | 1.0 / 1 |
| Nivel 1 | 3.5 / 7.1 | 4.0 / 22.0 | 642 / 662 | 1.0 / 1 |
| Nivel 2 | 8.6 / 14.0 | 8.2 / 28.0 | 646 / 667 | 1.0 / 1 |
| Nivel 3 | 17.3 / 29.2 | 13.2 / 24.0 | 646 / 676 | 1.2 / 3 |
| Repetición | 21.2 / 117.1* | 12.4 / 22.0 | 647 / 667 | 1.0 / 1 |

\* Pico puntual de una sola muestra de 5s (probablemente una ráfaga de requests concurrentes atendidas en más de un core); el resto de la corrida se mantiene por debajo del 30%.

**Infraestructura — Worker Server y Cloud SQL** (Cloud Monitoring, retroactivo, cruzado por ventana de tiempo de cada nivel — `loadtests/results/escenario2/infra/`):

| Nivel | CPU Worker Server (avg/max %) | CPU Cloud SQL (avg/max %) | Conexiones activas Cloud SQL (avg/max) |
|---|---|---|---|
| Línea base | 11.7 / 13.9 | 5.6 / 5.6 | 8.0 / 8 |
| Nivel 1 | 26.3 / 31.9 | 5.9 / 6.2 | 8.0 / 8 |
| Nivel 2 | 40.3 / 48.3 | 7.4 / 7.9 | 9.0 / 9 |
| Nivel 3 | 71.8 / 81.7 | 9.1 / 9.8 | 9.3 / 10 |
| Repetición | 43.6 / 84.1 | 7.1 / 10.3 | 9.0 / 9 |

**Hallazgo clave:** a diferencia de Escenario 1, acá **sí hay un componente que crece claramente con la carga hasta niveles altos de uso real**: Worker Server pasa de 11.7% a 71.8-84.1% de CPU (línea base → Nivel 3/Repetición), mientras Web Server (<30%) y Cloud SQL (<10%) se mantienen holgados en todo momento. Esto confirma con medición directa —no solo por inferencia de `media_processing_duration`— que el pipeline de transcodificación en Worker Server es el primer componente en acercarse a su límite.

**Throughput y checks:** 0 fallas HTTP y 0 checks fallidos en los 5 niveles (Línea base: 1040 reqs/7.1 req/s → Repetición: 46383 reqs/228 req/s). `media_processing_timeouts=0` y `media_segment_download_failures=0` en todos los niveles — ningún job quedó atascado ni ningún segmento HLS falló al descargar.

### Análisis por punto exigido por el enunciado

**Carga directa (URLs firmadas):** `create_resource` (autorización + URL firmada) se mantiene 112-120ms avg en todos los niveles — la API nunca es el cuello de botella de la carga. `upload_put` (transferencia directa a almacenamiento, no pasa por la API) es el paso más lento del flujo de subida (366-445ms avg), consistente con ser transferencia de archivo real contra el object storage, no cómputo de la API.

**Procesamiento asíncrono:** el tiempo desde carga completa hasta `available` crece con la carga — de ~3.4s (línea base) a ~5.4s avg / 9.5s p99 (Nivel 3) — pero se mantiene muy por debajo del umbral de referencia (45s) en todos los niveles. El crecimiento no es proporcional al de `PLAYBACK_VUS`/`UPLOAD_VUS` (que se multiplica x30), lo que sugiere que la concurrencia de workers (mantenida fija a propósito) empieza a ser el limitante del pipeline de transcodificación antes que la API o la base de datos — confirmado con CPU real de Worker Server (11.7% → 71.8-84.1% entre línea base y Nivel 3/Repetición, ver tabla de infraestructura arriba).

**Trabajos completados, reintentos y cola:** 0 timeouts y 0 rechazos de encolado (`media_enqueue_rejected`, sin muestras) en los 5 niveles — todo lo que se aceptó a la API terminó en `available`. No se observó profundidad ni antigüedad de la cola de asynq directamente (no hay panel de asynq monitoreado en esta entrega); la métrica usada como proxy es `media_processing_duration`.

**Consumo HLS:** `manifest` y `segment` se mantienen estables y dentro de umbral en los 5 niveles (segment p95 608-689ms contra umbral de referencia 1000ms), incluso con `PLAYBACK_VUS` en 300. `stream_url` p99 sube de 156ms a ~350-355ms desde Nivel 1 en adelante (polling mientras el video termina de procesarse), pero su p95 se mantiene bajo 130ms — el p99 alto es exactamente el patrón esperado del pequeño porcentaje de reproducciones que arrancan justo cuando el video todavía se está transcodificando.

**Componente que limita el flujo:** con los niveles probados (hasta `UPLOAD_VUS=10`/`PLAYBACK_VUS=300`), **no se alcanzó un punto de saturación real** — 0% de error HTTP en todos los niveles. Pero ya hay un componente claramente más cargado que el resto: Worker Server llega a 71.8-84.1% de CPU en Nivel 3/Repetición (medido directamente, ver tabla de infraestructura), mientras Web Server se mantiene bajo 30% y Cloud SQL bajo 10%. Con un nivel más de carga (más VUs de subida, que es lo que fuerza más transcodificaciones concurrentes) es esperable que Worker Server sea el primero en saturar. Con una CDN delante del almacenamiento de objetos, el tráfico de `manifest`/`segment` (ya el 90%+ de las requests en los niveles altos) dejaría de pasar por la API/almacenamiento directo, liberando esa capacidad para más subida y procesamiento concurrente; más capacidad de procesamiento (más workers o más CPU en Worker Server) atacaría directamente el único componente que mostró crecimiento real con la carga.

### Limitaciones del experimento

- **No se alcanzó saturación real** en ningún nivel probado — el máximo reportado (`UPLOAD_VUS=10`, `PLAYBACK_VUS=300`) no corresponde a la capacidad máxima de la plataforma, solo al techo probado en esta entrega.
- **Perfiles y escalera de la corrida documentada vs. código actual:** la corrida del 25 de septiembre usó el mismo archivo (~90KB/5s) para los 3 "perfiles" y una escalera HLS de una sola calidad; ese hallazgo ya está corregido en el código (3 archivos distintos + escalera multi-calidad, ver nota en la definición del escenario) pero **las cifras de este documento aún no reflejan la corrección**. Los videos nuevos siguen siendo sintéticos (patrón de prueba + ruido + tono), no contenido filmado.
- **No se midió tiempo hasta el primer cuadro ni interrupciones de reproducción con un reproductor real** — las métricas de manifiesto/segmento son peticiones HTTP, no reproducción real.
- **No se instrumentó la profundidad/antigüedad de la cola de asynq directamente**; se usó `media_processing_duration` como proxy.


## Propuesta de evolución

Los dos escenarios apuntan a componentes distintos, así que la evolución tiene dos frentes:

1. **Escenario 1 (académico): el cuello de botella está aguas abajo de la API** — `catalog`/`enroll`/`heartbeat`/`quiz_start` se degradan juntos y a la par entre Nivel 2 (150 VUs, sano) y Nivel 3 (400 VUs, p95~60s, 5-9% error), lo que apunta al pool de conexiones de la API hacia PostgreSQL o a la propia instancia de Cloud SQL (VM de 2 vCPU) saturándose bajo escritura+lectura concurrente. La medición que respalda esto: los cuatro endpoints comparten el mismo patrón de degradación pese a tener costos de negocio muy distintos (una lectura de catálogo vs. una escritura de heartbeat), lo cual descarta que sea un endpoint particular con una consulta cara. Propuesta: subir el tier de Cloud SQL (más vCPU/conexiones máximas) y/o aumentar el `max_open_conns` del pool de la API, y repetir el Nivel 3 para confirmar si el punto de quiebre se corre hacia arriba.
2. **Escenario 2 (multimedia): el cuello de botella es el pipeline de transcodificación, no la API ni el consumo HLS** — `media_processing_duration` crece de ~3.4s a ~5.4s avg (9.5s p99) entre línea base y Nivel 3, mientras la API (CPU Web Server <30% en el peor caso) y el consumo HLS (segment p95 estable en ~610-690ms) se mantienen sanos con 30x más carga. La medición que respalda esto: el único número que crece con la carga es justamente el que depende de la concurrencia fija de Worker Server. Propuesta: aumentar la concurrencia de workers/CPU de Worker Server y, para el consumo (ya el grueso del tráfico en los niveles altos), poner una CDN delante del almacenamiento de objetos — libera esa capacidad de la API/almacenamiento directo para más subida y procesamiento concurrente.

Ambas propuestas quedan pendientes de validar con una corrida de confirmación (fuera del alcance de esta entrega); ver limitaciones de cada escenario para lo que falta medir antes de tomarlas como definitivas (sobre todo memoria/disco/red, y granularidad más fina que el minuto de Cloud Monitoring).

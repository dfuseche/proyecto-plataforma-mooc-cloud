# Guía: Escenario 3 — ciclo de autoscaling (3A API y 3B workers)

Objetivo: producir en cada grupo un ciclo observable (1 instancia → creación automática → la réplica
nueva atiende o procesa → reducción automática), con línea de tiempo, métricas, verificación de
integridad y una comprobación controlada de retiro de un worker. La evidencia alimenta la sección
"Escenario 3" de [`capacity-planning/pruebas_de_carga_entrega3.md`](../../capacity-planning/pruebas_de_carga_entrega3.md).

Tiempo estimado: cambios previos (sección 2) de 1 a 2 días; cada corrida de 3A o 3B dura unos 50 minutos,
la comprobación 3B-R unos 20 minutos y la recolección de evidencia 1 hora por corrida.

---

## 0. Qué hay que demostrar

| # | Requisito del enunciado | Evidencia | Archivo |
|---|---|---|---|
| 1 | Iniciar con baja carga y 1 instancia operativa | Fase `base` de k6 + tamaño del grupo = 1 | `timeline.csv` |
| 2 | Creación **automática** de al menos 1 instancia | `recommendedSize` y `targetSize` suben sin intervención; operación `insert` de Compute | `timeline.csv`, `operaciones.json` |
| 3A | La réplica nueva **atiende solicitudes** | Instancia `HEALTHY` en el backend del balanceador + respuestas con `X-Served-By` de la réplica nueva | `timeline.csv`, `k6_console.log` |
| 3B | El worker nuevo **procesa trabajos** | Log "Iniciando transcodificación" con el nombre de la instancia nueva | `logs/worker.json` |
| 4 | Reducción automática al bajar la demanda | `targetSize` vuelve a 1 sin intervención | `timeline.csv` |
| 4B | Drenaje de la cola y trato seguro de trabajos activos | `pending`/`active` llegan a 0; logs de apagado ordenado | `monitoring/cola.csv`, `logs/worker.json` |
| 5 | Integridad durante el ciclo | `quiz_double_grading_detected = 0` (3A); reconciliación de trabajos (3B) | `summary.json`, `reconciliacion.md` |
| 6 | Separar el **tiempo de reacción** del **tiempo de incorporación** | Tabla de hitos T0…T9 (sección 9) | `hitos.md` |
| 7 | Retiro controlado de un worker con trabajo activo | 3B-R: logs de la VM retirada + el mismo trabajo terminado en otro worker | carpeta `..._3B-R/` |
| 8 | Configuración y resultados **diferenciados** 3A / 3B | YAML exportados de cada grupo y política | `config/` |

Escalar el grupo a mano no cuenta como autoscaling. En 3A y 3B no se usa `resize`; solo en 3B-R se
retira una instancia a mano, y eso se declara en el informe.

---

## 1. Estado actual y brechas

**Lo que ya existe y sirve:** los grupos `mooc-api-mig` y `mooc-worker-mig` (1 a 3 instancias, CPU 60 %,
inicialización 120 s, en [`scripts/gcp/config.ps1`](../../scripts/gcp/config.ps1)), el arranque automático
([`deploy/startup/startup.sh`](../../deploy/startup/startup.sh)), Redis fuera de los grupos y la idempotencia
de los trabajos: `TaskID` igual al ID del recurso, `MaxRetry(3)` y claves HLS deterministas
`hls/<resource_id>/…`, de modo que un reintento sobrescribe los mismos objetos y no crea derivados nuevos.

**Brechas que impiden una evidencia válida:**

| # | Brecha | Consecuencia en el Escenario 3 | Cambio |
|---|---|---|---|
| B1 | El grupo de workers escala por CPU | El enunciado pide para workers una señal de demanda de procesamiento (profundidad o antigüedad de la cola). Con CPU, un solo worker con 2 transcodificaciones ya pide 2 VMs aunque no haya nada en cola, y un grupo con la cola vacía pero con trabajos activos puede reducirse | C1, C2, C3 |
| B2 | La API no identifica qué instancia respondió | No se puede probar que la réplica nueva atiende tráfico | C4 |
| B3 | Los logs de los contenedores quedan en el disco de la VM | Cuando el autoscaler borra una VM se pierde la evidencia (apagado del worker, peticiones atendidas) | C5 |
| B4 | `encender.ps1` vuelve a aplicar CPU 60 % a los dos grupos | Borraría la política de cola del grupo de workers | C3 |
| B5 | El apagado del worker usa los valores por defecto (asynq 8 s, Docker 10 s) | Funciona por coincidencia, sin estar documentado ni verificado | C6 |
| B6 | En **audio** se sube `master.m3u8` antes que los segmentos (orden alfabético de `os.ReadDir`) | Si el worker muere a mitad de la subida, el reintento encuentra `master.m3u8`, se salta el trabajo y marca el recurso como completado con segmentos faltantes. En video funciona solo porque los nombres empiezan por un dígito | C7 |
| B7 | No se registra por trabajo cuánto esperó en cola y cuánto tardó en procesarse | No se pueden separar la espera en cola y el procesamiento, como pide el enunciado | C8 |
| B8 | Cada réplica de la API aplica las migraciones al arrancar, sin lock, y no hace apagado ordenado | En 3A las réplicas nuevas arrancan bajo carga y ejecutan migraciones a la vez; al retirarlas se cortan peticiones en curso | C9, C10 |
| B9 | La plantilla usa la imagen `:latest` | Un despliegue durante la corrida haría que las réplicas nuevas ejecuten otra versión | C11 |
| B10 | No hay scripts de k6 para 3A y 3B ni captura de la línea de tiempo | — | C12, C13, C14 |
| B11 | Sin Ops Agent no hay métrica de memoria, y `monitor_vm.sh` requiere SSH a cada VM, algo inviable con instancias efímeras | Falta la memoria de API y workers | C15 |

---

## 2. Plan de cambios

**Prioridad:** P0 = sin esto 3A/3B no producen evidencia válida; P1 = necesario para la integridad y el
rigor que se evalúan; P2 = opcional.

**Estado:** C1, C2, C3, C4 y C5 están implementados. Cómo desplegarlos y comprobarlos en GCP:
[`prueba-cambios-autoscaling.md`](prueba-cambios-autoscaling.md). El resto sigue pendiente.

### C1 (P0, implementado) Exportador de métricas de la cola — [`backend/cmd/queue-metrics/main.go`](../../backend/cmd/queue-metrics/main.go)

- Binario nuevo en la misma imagen (`/app/queue-metrics`, ver [`backend/Dockerfile`](../../backend/Dockerfile));
  la lógica está en [`backend/internal/queuemetrics/`](../../backend/internal/queuemetrics/), con pruebas.
- Cada `QUEUE_METRICS_INTERVAL` (por defecto 15 s) llama a `asynq.Inspector.GetQueueInfo` para las colas
  que consume el worker (`worker.QueuePriorities`: `critical`, `default` y `low`; si una cola aún no existe
  cuenta 0), suma los valores y los publica en Cloud Monitoring con el recurso `global`:

  | Métrica `custom.googleapis.com/mooc/asynq/…` | Valor | Uso |
  |---|---|---|
  | `backlog` | pending + active | **Señal de autoscaling** del grupo de workers |
  | `pending`, `active`, `retry`, `archived` | Tareas en cada estado | Análisis; `archived` = DLQ |
  | `oldest_pending_age_seconds` | Máximo de `QueueInfo.Latency` | Antigüedad de la cola |
  | `processed_total`, `failed_total` | Acumulados de `QueueInfo` | Tasa de procesamiento y reintentos |

- Publica con la API REST (`POST https://monitoring.googleapis.com/v3/projects/<proyecto>/timeSeries`)
  usando el token y el proyecto del servidor de metadatos, igual que `startup.sh`. Así no se agrega ninguna
  dependencia a `go.mod`. Con `QUEUE_METRICS_DRY_RUN=true` solo escribe en el log (fuera de GCP).
- También escribe en stdout una línea por muestra (`[QUEUE-METRICS] backlog=… pending=… active=… …`), que
  queda en Cloud Logging como serie de respaldo.
- Se ejecuta en la **VM de Redis** (fuera de los grupos) como contenedor `mooc-queue-metrics` con
  `--restart=always`. Lo instala [`scripts/gcp/exportador-cola.ps1`](../../scripts/gcp/exportador-cola.ps1),
  que copia y ejecuta [`deploy/scripts/queue-metrics.sh`](../../deploy/scripts/queue-metrics.sh) en esa VM.
  Su cuenta de servicio necesita `roles/monitoring.metricWriter`, `roles/logging.logWriter` y
  `roles/artifactregistry.reader` ([`revisar-permisos.ps1`](../../scripts/gcp/revisar-permisos.ps1) lo comprueba).
- **Por qué pending + active:** "una cola sin trabajos pendientes puede tener trabajos en ejecución"
  (enunciado). Al contar los activos, el autoscaler no recomienda retirar consumidores mientras procesan.
- Prueba unitaria: suma de colas y armado del cuerpo JSON, sin red.
- Con el exportador caído, la métrica deja de tener datos. Verificar en la consola qué hace el autoscaler
  en ese caso y registrar el exportador como punto único de falla junto a Redis (deuda D-12).

### C2 (P0, implementado) Política del grupo de workers basada en la cola

La aplican `Set-WorkerAutoscaling` ([`config.ps1`](../../scripts/gcp/config.ps1)), `encender.ps1` y
[`aplicar-autoscaling.ps1`](../../scripts/gcp/aplicar-autoscaling.ps1). Equivale a:

```bash
gcloud compute instance-groups managed set-autoscaling mooc-worker-mig --zone=us-central1-a \
  --min-num-replicas=1 --max-num-replicas=3 \
  --cool-down-period=180 \
  --update-stackdriver-metric=custom.googleapis.com/mooc/asynq/backlog \
  --stackdriver-metric-filter='resource.type = global' \
  --stackdriver-metric-single-instance-assignment=2 \
  --scale-in-control=max-scaled-in-replicas=1,time-window=300 \
  --mode=on
```

| Parámetro | Valor | Justificación |
|---|---|---|
| Señal | `backlog` = pending + active (métrica por grupo) | Representa la demanda de procesamiento, no el consumo de CPU |
| Objetivo | 2 trabajos por VM (`single-instance-assignment`) | Es `WORKER_CONCURRENCY`: cada e2-highcpu-2 procesa 2 a la vez y el resto espera. VMs deseadas = ⌈backlog / 2⌉: 0–2 → 1 VM, 3–4 → 2, ≥ 5 → 3 |
| Inicialización | 180 s | Arranque medido de la VM, Docker y el pull de la imagen (confirmar con la medición de la sección 3.3) |
| Estabilización | 10 min (por defecto en GCE) | La reducción usa el pico de los últimos 10 min: evita oscilaciones entre ráfagas de subidas |
| Control de reducción | Máximo 1 VM cada 300 s | Reducción escalonada y observable (3 → 2 → 1) |
| Límites | 1 a 3 | Con 3 VMs y la cola creciendo, los trabajos esperan y crece `oldest_pending_age_seconds` (se observa en 3B) |

### C3 (P0, implementado) Scripts de encendido con una política por grupo

[`config.ps1`](../../scripts/gcp/config.ps1) define `Set-ApiAutoscaling` (CPU 60 %, 120 s) y
`Set-WorkerAutoscaling` (C2), con `$WorkerBacklogMetric`, `$WorkerJobsPerVm = 2`, `$WorkerCoolDown = 180`
y el control de reducción (1 VM cada 300 s en los dos grupos). [`encender.ps1`](../../scripts/gcp/encender.ps1)
y [`aplicar-autoscaling.ps1`](../../scripts/gcp/aplicar-autoscaling.ps1) las usan.
[`estado.ps1`](../../scripts/gcp/estado.ps1) muestra la política efectiva, el `recommendedSize` y los avisos
del autoscaler de cada grupo; con `-Cada 15` repite cada 15 s.

### C4 (P0, implementado) Identificador de instancia y versión en las respuestas de la API

- [`internal/middleware/served_by.go`](../../backend/internal/middleware/served_by.go) agrega
  `X-Served-By: <INSTANCE_NAME>` (si falta, usa `os.Hostname()`, que en Docker es el ID del contenedor) y
  `X-App-Version: <APP_VERSION>`. Se registra junto a `SecurityHeaders` en `cmd/api/main.go`; tiene pruebas.
- `Dockerfile`: `ARG APP_VERSION` + `ENV APP_VERSION` en la etapa final (el CD ya pasa el SHA del commit).
- `startup.sh`: `INSTANCE_NAME=$(md instance/name)` y `-e INSTANCE_NAME="$INSTANCE_NAME"` en los dos roles.
- No es información sensible: es el nombre técnico de la VM, ya visible en la consola, y no expone IPs ni secretos.

### C5 (P0, implementado) Logs fuera de la VM

- `startup.sh` arranca los contenedores con `--log-driver=gcplogs`. Cada línea queda en Cloud Logging
  (`logName=…/gcplogs-docker-driver`, con `jsonPayload.instance.name` y `jsonPayload.container.name`)
  y sobrevive al borrado de la VM. Si el driver no puede autenticarse, `docker run` falla y el script
  arranca el contenedor con el driver por defecto, para no dejar la VM sin servicio.
- La cuenta de servicio de las plantillas necesita `roles/logging.logWriter`.
- Las plantillas guardan una copia del `startup-script`: [`actualizar-plantillas.ps1`](../../scripts/gcp/actualizar-plantillas.ps1)
  crea plantillas nuevas a partir de las actuales, las asigna y reemplaza las instancias.
- Verificación: Logs Explorer con `logName:"gcplogs-docker-driver" AND jsonPayload.instance.name:"mooc-worker-mig"`.

### C6 (P1) Apagado ordenado del worker

- `cmd/worker/main.go`: `asynq.Config{ShutdownTimeout: cfg.WorkerShutdownTimeout}`, con la variable
  `WORKER_SHUTDOWN_TIMEOUT` (por defecto 60 s) en `internal/config/config.go`.
- `startup.sh` (worker): `--stop-timeout 75` en `docker run`, para que Docker no mate el proceso antes de
  que asynq termine.
- Comportamiento esperado al retirar la VM: SIGTERM → asynq deja de tomar tareas nuevas → espera hasta 60 s
  a las activas → las que no terminaron **vuelven a `pending` sin consumir un reintento** → otro worker
  las toma. Si la VM muere sin apagado ordenado, el *lease* de 30 s vence y el *recoverer* las pasa a `retry`.
- Restricción: GCE da cerca de 90 s (mejor esfuerzo) para apagar una VM que se borra; 60 s + margen cabe.
  Se verifica en 3B-R.

### C7 (P1) Subir `master.m3u8` siempre al final

En [`backend/internal/worker/processor.go`](../../backend/internal/worker/processor.go), separar el orden
de subida en una función pura (`uploadOrder(names) []string`, con `master.m3u8` al final) y probarla.
Así la comprobación de idempotencia ("si existe `master.m3u8`, ya terminó") es correcta en audio y en video.

### C8 (P1) Espera en cola y procesamiento por trabajo

- `worker/task.go`: agregar `EnqueuedAt time.Time` (`json:"enqueued_at,omitempty"`) al payload; lo llena
  `NewMediaTranscodeHLSTask`. Es compatible con tareas ya encoladas.
- `processor.go`: al terminar, registrar
  `[WORKER-METRICS] resource=<id> instancia=<vm> reintento=<n> espera_cola_ms=… descarga_ms=… ffmpeg_ms=… subida_ms=… total_ms=…`.
- Estas líneas, extraídas de Cloud Logging, separan la espera en cola del procesamiento. El tiempo hasta
  `available` lo mide k6 (`media_processing_duration`).

### C9 (P1) Migraciones con lock

En `runMigrations` (`cmd/api/main.go`), tomar una conexión dedicada (`db.Conn`), ejecutar
`SELECT pg_advisory_lock(<constante>)`, aplicar las migraciones y liberar el lock con `pg_advisory_unlock`.
Las réplicas que arrancan a la vez esperan en vez de aplicar el SQL simultáneamente. Cumple el requisito
de "migraciones controladas".

### C10 (P1) Retiro limpio de una réplica de la API

- `cmd/api/main.go`: `signal.NotifyContext(SIGTERM, SIGINT)` + `srv.Shutdown(ctx)` con un límite de 20 s.
  `startup.sh` (api): `--stop-timeout 30`.
- Backend service: `gcloud compute backend-services update mooc-api-backend-service --global --connection-draining-timeout=30`.
  Al retirar una réplica, el balanceador deja de enviarle peticiones nuevas y espera hasta 30 s a las que
  están en curso antes de que el grupo borre la VM (las peticiones normales duran menos de 1 s).

### C11 (P1) Versión fija por corrida

Antes de cada sesión de pruebas, actualizar el metadato `mooc-image` de las plantillas a
`…/mooc/backend:<sha>` en lugar de `:latest`, o no hacer *push* a `main` durante las corridas (el CD
desplegaría en medio de la prueba). `X-App-Version` (C4) prueba que todas las réplicas ejecutaron la misma versión.

### C12 (P0) `loadtests/k6/autoscaling-api.js` (3A)

- Reutiliza el tráfico del Escenario 1: `import iteracion, { setup } from './load-test.js'`. k6 ignora el
  `options` del módulo importado y conserva los `responseCallback` de los 409 esperados.
- Executor `ramping-arrival-rate` (modelo abierto: la demanda se fija en iteraciones/s y no cae cuando sube
  la latencia):

  | Fase | Duración | Tasa |
  |---|---|---|
  | `base` | 5 min | `LOW_RATE` (p. ej. 3 it/s) |
  | `subida` | 2 min | Rampa hasta `HIGH_RATE` (sale de la calibración, sección 5.1) |
  | `sostenido` | 15 min | `HIGH_RATE` |
  | `bajada` | 1 min | Rampa hasta `LOW_RATE` |
  | `enfriamiento` | 22 min | `LOW_RATE` (cubre la estabilización de 10 min y la reducción 3 → 2 → 1) |

- Etiqueta `fase` en todas las peticiones (`exec.vu.metrics.tags.fase`, calculada con el tiempo transcurrido
  desde `exec.scenario.startTime`) y umbrales "vacíos" (`max>=0`) por fase, para que `--summary-export`
  traiga p50/p95/p99, throughput y errores **por fase**.
- Escenario paralelo `sonda_instancias` (2 req/s, `GET /api/v1/courses/<COURSE_ID>`): escribe
  `SONDA,<iso>,<fase>,<status>,<X-Served-By>,<X-App-Version>,<ms>` con `console.log`. Como el balanceador
  reparte igual todas las peticiones, la sonda es una muestra de la distribución real entre réplicas.
- Al cambiar de fase escribe `FASE,<iso>,<nombre>` (marca T0 y T6 con precisión).
- Modo `MODO=calibracion`: escalones `STEP_RATES` (p. ej. `5,10,20,30,40,50`) de 2 min cada uno.
- `preAllocatedVUs ≈ 3 × HIGH_RATE`, `maxVUs ≈ 8 × HIGH_RATE` (cada iteración dura de 1 a 3 s por el *think time*).
- `QUIZ_ATTEMPT_FRACTION=0.05`: con 300 usuarios × 3 intentos hay 900 intentos. A 30 it/s × 25 % × 5 %
  ≈ 0,4 quiz/s duran unos 38 min; con 0,3 se agotarían en 7 min y el resto solo mediría 409 esperados.

### C13 (P0) `loadtests/k6/autoscaling-workers.js` (3B)

- En [`media-load-test.js`](../../loadtests/k6/media-load-test.js), exportar `crearYSubirVideo`,
  `esperarManifiesto`, `getTeacherToken` y `profileByName` (solo agregar `export`) para reutilizarlas.
- Executor `ramping-arrival-rate` sobre la llegada de trabajos, con `TIME_UNIT` configurable (`1m` o `10m`
  para tasas fraccionarias):

  | Fase | Duración | Llegadas |
  |---|---|---|
  | `base` | 5 min | `LOW_RATE` (≈ 0,5 μ; μ = capacidad de 1 VM, sección 5.2) |
  | `subida` | 1 min | Rampa hasta `HIGH_RATE` (≈ 2,5 μ: más de lo que procesan 2 VMs y menos que 3) |
  | `sostenido` | 12 min | `HIGH_RATE` |
  | `corte` | — | Tasa 0 (la demanda cae de golpe) |
  | `drenaje` | 25 min | 0 llegadas; los VUs siguen esperando sus trabajos |

- Cada iteración crea el recurso, hace el PUT directo, `complete-upload` y espera el manifiesto. Escribe
  `JOB,<iso>,<resource_id>,aceptado` y `JOB,<iso>,<resource_id>,disponible|timeout`.
- `PROFILE=pesado` por defecto (1080p, 4 renditions): costo por trabajo estable y conocido.
- `POLL_INTERVAL_SECONDS=10` y `MAX_WAIT_FOR_MANIFEST_SECONDS=1800`. Con 3 s de polling y unos 100 VUs
  esperando, la sonda subiría la CPU de la API y podría escalarla en medio de 3B.
- `gracefulStop: '30m'` para no cortar trabajos en espera al terminar el escenario.
- Modo `MODO=rafaga N=6` (calibración y 3B-R): sube N trabajos de una vez y espera a que terminen.

### C14 (P0) Línea de tiempo y análisis

- `loadtests/monitoring/timeline_autoscaling.sh`: cada 15 s agrega a un CSV, por grupo,
  `ts, grupo, target_size, recommended_size, autoscaler_status, instancias (nombre:estado:acción)` y, para la
  API, el estado de cada backend (`gcloud compute backend-services get-health mooc-api-backend-service --global`).
  Los dos primeros valores salen de `gcloud compute instance-groups managed describe` (bloque `autoscaler`)
  o de la API REST de `autoscalers`. Corre desde Cloud Shell o desde un equipo con gcloud, nunca en las VMs de aplicación.
- `loadtests/monitoring/exportar_metricas_gcp.sh`: descarga de Cloud Monitoring (REST `timeSeries.list`
  con `gcloud auth print-access-token`), para la ventana de la corrida y alineada a 60 s:
  CPU por instancia, memoria (si hay Ops Agent), red, `loadbalancing.googleapis.com/https/request_count`
  y `total_latencies`, CPU y conexiones de Cloud SQL y las métricas `custom.googleapis.com/mooc/asynq/*`.
- `loadtests/analizar_escenario3.py`: une `timeline.csv`, `k6_console.log` (FASE/SONDA/JOB), las métricas y
  los logs en una línea de tiempo por grupo; calcula los hitos de la sección 9 y genera las gráficas (PNG).

### C15 (P1) Memoria con Ops Agent

Instalar Ops Agent en `startup.sh` solo si falta (`add-google-cloud-ops-agent-repo.sh --also-install`).
Suma cerca de 1 min al primer arranque; medirlo y reflejarlo en el periodo de inicialización. La alternativa
(P2) es una imagen de VM propia con Docker y Ops Agent preinstalados, que además acorta el arranque.

### C16 (P2) Panel de Cloud Monitoring versionado

`loadtests/monitoring/dashboard_escenario3.json` (`gcloud monitoring dashboards create --config-from-file=…`)
con: CPU por instancia de cada grupo, tamaño de los grupos, `backlog`/`pending`/`active`,
`oldest_pending_age_seconds`, peticiones y latencia del balanceador y CPU y conexiones de Cloud SQL.
Es la pantalla principal para grabar el autoscaling en vivo.

### Orden de trabajo sugerido

| Paso | Cambios | Depende de | Validación antes de las corridas oficiales |
|---|---|---|---|
| 1 | C4, C5 (hechos); C6, C7, C8, C9, C10 (código + `startup.sh`) | — | CI en verde; `curl -skI https://<LB>/health` muestra `X-Served-By` y `X-App-Version`; logs visibles en Cloud Logging |
| 2 | C1, C2, C3 (hechos) | Paso 1 (imagen con `/app/queue-metrics`) | [`prueba-cambios-autoscaling.md`](prueba-cambios-autoscaling.md), pasos 5 a 7 |
| 3 | C12, C13, C14 | Pasos 1 y 2 | Humo de 5 min de cada script |
| 4 | C11, C15, C16 | — | `estado.ps1` y el panel |

---

## 3. Preparación (en cada sesión de pruebas)

### 3.1 Encender y fijar la versión

```powershell
.\scripts\gcp\encender.ps1      # ya aplica la política de cola a los workers (C3)
.\scripts\gcp\estado.ps1
```

Confirmar en el pie del frontend el hash del commit que se va a evaluar y que no haya *push* a `main`
pendiente de desplegar (C11).

### 3.2 Variables (bash, en Cloud Shell o Git Bash con gcloud)

```bash
export PROJECT=proyecto-plataforma-mooc ZONE=us-central1-a
export API_MIG=mooc-api-mig WORKER_MIG=mooc-worker-mig BACKEND=mooc-api-backend-service
export LB=https://34.49.211.71
export RUN=$(date -u +%Y%m%d_%H%M%S)_3A          # _3B o _3B-R según la corrida
export OUT=loadtests/results/escenario3/$RUN && mkdir -p $OUT/{config,monitoring,logs}
gcloud config set project $PROJECT
```

### 3.3 Exportar la configuración efectiva (evidencia de "configuración diferenciada")

```bash
for MIG in $API_MIG $WORKER_MIG; do
  gcloud compute instance-groups managed describe $MIG --zone $ZONE --format=yaml > $OUT/config/$MIG.yaml
  TPL=$(gcloud compute instance-groups managed describe $MIG --zone $ZONE --format='value(instanceTemplate.basename())')
  gcloud compute instance-templates describe $TPL --format=yaml > $OUT/config/template-$MIG.yaml
done
gcloud compute backend-services describe $BACKEND --global --format=yaml > $OUT/config/backend-service.yaml
for HC in $(gcloud compute health-checks list --format='value(name)'); do
  gcloud compute health-checks describe $HC --format=yaml > $OUT/config/health-check-$HC.yaml
done
```

Revisar que ningún archivo tenga secretos antes de hacer commit: los metadatos de la plantilla solo
llevan el **nombre** del secreto, no su valor.

**Tiempo de arranque (una vez, antes de fijar el periodo de inicialización):** con `timeline_autoscaling.sh`
corriendo, reemplazar una instancia de cada grupo sin bajar capacidad y medir de la creación al estado
`HEALTHY` (API) o al log "Worker escuchando en la cola" (worker):

```bash
gcloud compute instance-groups managed rolling-action replace $API_MIG --zone $ZONE --max-surge=1 --max-unavailable=0
```

"Lista" significa: API = `/health` responde 200 por el balanceador, lo que ocurre después de las migraciones
y de abrir el puerto; worker = asynq conectado a Redis y escuchando.

### 3.4 Datos

```bash
psql "$DATABASE_URL" -f loadtests/seed/reset_quiz_attempts.sql      # antes de cada 3A
```

En 3B, anotar la hora de inicio: la reconciliación filtra los recursos por título (`Autoscaling 3B %`) y por fecha.

### 3.5 Generador de carga

La misma VM `mooc-load-generator` de E1/E2 (`us-central1-a`). Registrar `k6 version`, el tipo de máquina y
`git rev-parse HEAD`. Durante la corrida, su CPU debe quedar por debajo del 70 % (Metrics Explorer o
`vmstat 5`). Si la supera, el generador limita la medición y la corrida no vale.

### 3.6 Puesto de captura en vivo

Grabar la pantalla completa durante todo el ciclo (OBS, o Win + Alt + R en Windows) con cuatro ventanas:

1. **Consola → Instance groups → `mooc-api-mig` (o `mooc-worker-mig`) → pestaña Instancias**, que muestra
   las VMs con su estado (`Creating`, `Verifying`, `Running`, `Deleting`).
2. **Load balancing → backend `mooc-api-backend-service`**: instancias sanas (3A) o **Logs Explorer en
   modo *Stream*** con `jsonPayload.message:"[WORKER"` (3B).
3. **Panel de Monitoring** (C16) con un rango de 1 h y actualización automática.
4. **Terminal** con `tail -f $OUT/timeline.csv` y otra con k6.

Para el video basta un fragmento acelerado; la grabación completa se conserva como evidencia.

---

## 4. Criterios definidos antes de las pruebas

| | 3A (API) | 3B (workers) |
|---|---|---|
| **Éxito** | ≥ 1 creación automática; réplica nueva `HEALTHY` y con ≥ 10 % de las respuestas de la sonda en `sostenido`; vuelta automática a 1; errores de infraestructura < 1 %; `quiz_double_grading_detected = 0` | ≥ 1 creación automática por la señal de cola; el worker nuevo procesa ≥ 1 trabajo; la cola drena a 0; vuelta automática a 1; aceptados = completados + fallos diagnosticables; 0 duplicados; `archived = 0` |
| **Degradación** (se reporta, no detiene) | p95 > 1 s o errores entre 1 % y 5 % mientras arranca la réplica | `oldest_pending_age_seconds` > 10 min; tiempo hasta `available` p95 > 15 min |
| **Parada** | Errores > 20 % durante 2 min; CPU de Cloud SQL > 95 % durante 3 min; CPU del generador > 85 % | `archived` creciendo; `failed_total` creciendo sin recuperación; Redis sin memoria; más del 10 % de los trabajos con *timeout* |

**Clasificación de errores** (vale para todo el informe):

- **Infraestructura:** 502/503/504 del balanceador, *timeouts*, errores de conexión.
- **Funcionales:** `check` fallidos, 4xx no esperados y 5xx de la aplicación.
- **Rechazos esperados:** 409 por inscripción repetida, por límite de intentos o por envío duplicado de un quiz.

Un 200 no basta: la integridad se valida con `quiz_double_grading_detected` y con la reconciliación de 3B.

---

## 5. Calibración (antes de las corridas oficiales)

### 5.1 3A: cuánta carga lleva la CPU de 1 réplica por encima del 60 %

```bash
gcloud compute instance-groups managed update-autoscaling $API_MIG --zone $ZONE --mode=off   # se queda en 1
k6 run --insecure-skip-tls-verify -e BASE_URL=$LB -e MODO=calibracion -e STEP_RATES=5,10,20,30,40,50 \
  --summary-export=$OUT/calibracion.json loadtests/k6/autoscaling-api.js
gcloud compute instance-groups managed update-autoscaling $API_MIG --zone $ZONE --mode=on
```

En cada escalón, anotar la CPU de la réplica (Metrics Explorer, `compute.googleapis.com/instance/cpu/utilization`
a 1 min), p95, errores y la CPU de Cloud SQL. **`HIGH_RATE`** es el menor escalón en que la CPU de la réplica
supera ≈ 80 % (1,3 veces el objetivo) con errores < 1 % y Cloud SQL por debajo del 80 %. Con esa carga el
autoscaler pide 2 réplicas. Llegar a 3 exige que la media con 2 réplicas siga por encima del 60 %; no es
obligatorio, basta 1 creación adicional.

**Si la CPU nunca llega al 60 % aunque la latencia empeora**, la API está limitada por la base de datos
(como en E1 con 400 VUs) y la CPU no es la señal adecuada. Plan B: escalar por peticiones por instancia,
una señal que el enunciado también admite.

```bash
gcloud compute backend-services update-backend $BACKEND --global \
  --instance-group=$API_MIG --instance-group-zone=$ZONE \
  --balancing-mode=RATE --max-rate-per-instance=<R>
gcloud compute instance-groups managed set-autoscaling $API_MIG --zone $ZONE \
  --min-num-replicas=1 --max-num-replicas=3 --cool-down-period=120 \
  --target-load-balancing-utilization=0.8 --target-cpu-utilization=0.6 --mode=on
```

`R` = las req/s que 1 réplica atiende con p95 sano en la calibración; el autoscaler escala al llegar al
80 % de `R`. Documentar el cambio y su justificación en el informe y en `arquitectura.md`.

### 5.2 3B: capacidad μ de 1 worker

```bash
gcloud compute instance-groups managed update-autoscaling $WORKER_MIG --zone $ZONE --mode=off
k6 run --insecure-skip-tls-verify -e BASE_URL=$LB -e MODO=rafaga -e N=6 -e PROFILE=pesado \
  --console-output=$OUT/calibracion_console.log loadtests/k6/autoscaling-workers.js
gcloud compute instance-groups managed update-autoscaling $WORKER_MIG --zone $ZONE --mode=on
```

μ = 6 / (minutos desde el primer `aceptado` hasta el último `disponible`). Ejemplo: si 6 trabajos pesados
tardan 6 min, μ = 1 trabajo/min, `LOW_RATE` = 0,5/min y `HIGH_RATE` = 2,5/min. Con `TIME_UNIT=10m` eso
queda `LOW_RATE=5` y `HIGH_RATE=25`.

---

## 6. Ejecución 3A — escalamiento de la API

**Antes:** los dos grupos en 1 instancia y estables (`estado.ps1`), autoscaler `on`, `backlog` = 0 y
quizzes reiniciados (3.4).

```bash
# Terminal 1 (Cloud Shell o equipo con gcloud)
./loadtests/monitoring/timeline_autoscaling.sh --out $OUT/timeline.csv --interval 15

# Terminal 2 (VM generadora)
K6_WEB_DASHBOARD=true K6_WEB_DASHBOARD_EXPORT=$OUT/dashboard.html \
k6 run --insecure-skip-tls-verify \
  -e BASE_URL=$LB -e LOW_RATE=3 -e HIGH_RATE=<calibrado> -e POOL_SIZE=300 -e QUIZ_ATTEMPT_FRACTION=0.05 \
  --summary-export=$OUT/summary.json --console-output=$OUT/k6_console.log \
  --out csv=$OUT/k6_raw.csv.gz \
  loadtests/k6/autoscaling-api.js 2>&1 | tee $OUT/k6.log
```

El *setup* inicia sesión con 300 usuarios espaciados por el límite de tasa (≈ 3,5 min). La fase `base`
empieza al terminar.

**Qué observar y capturar:**

| Minuto (desde `base`) | Qué debe pasar | Captura |
|---|---|---|
| 0–5 | 1 réplica `HEALTHY`, CPU baja, sonda con un solo `X-Served-By` | Grupo y backend con 1 instancia |
| 5–7 | Sube la demanda y la CPU pasa del 60 % (**T0, T1**) | Panel: CPU cruzando la línea del objetivo |
| ≈ 7–9 | `recommendedSize` 2 → `targetSize` 2 → VM en `Creating` (**T2**) | Lista de instancias con la VM nueva |
| +1–2 min | VM `Running` (**T3**); log "Servidor API … iniciado" (**T4**) | — |
| +≤ 1 min | Backend `HEALTHY` y primera línea `SONDA` con el nombre nuevo (**T5**) | Backend con 2 instancias sanas |
| 9–22 | Reparto entre réplicas; la latencia se estabiliza; posible 3.ª réplica | Panel |
| 22–23 | Baja la demanda (**T6**) | — |
| ≈ 32–40 | Tras la estabilización: `targetSize` baja de a 1 (**T7**), VM en `Deleting`, el balanceador drena (**T8, T9**) | Lista de instancias en `Deleting` |
| fin | 1 réplica. El grupo de workers siguió en 1 todo el tiempo (independencia de los grupos) | `estado.ps1` |

Al terminar, detener `timeline_autoscaling.sh` (Ctrl+C) y pasar a la sección 8.

---

## 7. Ejecución 3B — escalamiento de workers

**Antes:** `backlog` = 0 (Metrics Explorer), los dos grupos en 1 instancia, autoscaler `on` con la
política de cola (C2) y la hora de inicio anotada (UTC).

```bash
./loadtests/monitoring/timeline_autoscaling.sh --out $OUT/timeline.csv --interval 15     # terminal 1

K6_WEB_DASHBOARD=true K6_WEB_DASHBOARD_EXPORT=$OUT/dashboard.html \
k6 run --insecure-skip-tls-verify \
  -e BASE_URL=$LB -e PROFILE=pesado -e TIME_UNIT=10m -e LOW_RATE=<calibrado> -e HIGH_RATE=<calibrado> \
  -e POLL_INTERVAL_SECONDS=10 -e MAX_WAIT_FOR_MANIFEST_SECONDS=1800 \
  --summary-export=$OUT/summary.json --console-output=$OUT/k6_console.log \
  loadtests/k6/autoscaling-workers.js 2>&1 | tee $OUT/k6.log                              # terminal 2
```

**Qué observar y capturar:**

| Momento | Qué debe pasar | Captura |
|---|---|---|
| `base` | `backlog` entre 0 y 2, 1 worker | Panel de la cola |
| Inicio de `subida` (**T0**) | `backlog` ≥ 3 (**T1**) → `recommendedSize` 2 (**T2**); con ≥ 5 → 3 | Panel: `backlog` y tamaño del grupo |
| +2–4 min | Worker nuevo `Running` (**T3**), "Worker escuchando…" (**T4**), primer "Iniciando transcodificación … instancia=<nuevo>" (**T5**) | Logs Explorer en *stream* |
| `sostenido` | Concurrencia efectiva = 2 × workers activos; `oldest_pending_age_seconds` se estabiliza o baja | Panel |
| `corte` (**T6**) | `pending` → 0 y después `active` → 0: la cola drena | Panel |
| +10 min tras `backlog` = 0 | Reducción 3 → 2 (**T7**) y, 5 min después, 2 → 1. La VM retirada apaga asynq de forma ordenada (**T8, T9**) | Lista de instancias + logs de apagado |
| fin | `archived` = 0, `retry` = 0 | Panel |

Si la API también escala durante 3B (por el polling), registrarlo y explicarlo: no invalida 3B, pero hay que declararlo.

### 7.1 3B-R — retiro controlado de un worker con un trabajo activo

Es una comprobación aparte de la reducción automática y no la reemplaza.

1. `gcloud compute instance-groups managed update-autoscaling $WORKER_MIG --zone $ZONE --min-num-replicas=2`
   y esperar 2 workers listos. Este ajuste manual es temporal y se declara.
2. Subir 1 video pesado: `k6 run … -e MODO=rafaga -e N=1 loadtests/k6/autoscaling-workers.js`. Anotar el `resource_id`.
3. En Logs Explorer: `jsonPayload.message:"<resource_id>"` → muestra qué instancia lo tomó.
4. **Unos 10 s después** de "Iniciando transcodificación" (para que no alcance a terminar), retirar esa VM:
   ```bash
   gcloud compute instance-groups managed delete-instances $WORKER_MIG --zone $ZONE --instances=<vm>
   ```
5. Esperado: en los logs de la VM retirada, el apagado de asynq (deja de tomar tareas, espera y devuelve la
   tarea a la cola). Después, en el otro worker, "Iniciando transcodificación … <resource_id>" (segundo
   intento) y `[WORKER-SUCCESS]` **una sola vez**. El autoscaler repone la segunda instancia porque el mínimo es 2.
6. Verificar:
   ```sql
   SELECT id, processing_status, media_url, updated_at FROM course_resources WHERE id = '<resource_id>';
   ```
   ```bash
   gcloud storage ls gs://<bucket>/hls/<resource_id>/ | wc -l    # pesado 20 s: 1 master + 4 playlists + 4×4 segmentos = 21
   ```
   El video se reproduce en el navegador y el contador de `[WORKER-SUCCESS]` para ese recurso es 1.
7. Opcional (recuperación sin apagado ordenado): repetir con `gcloud compute instances reset <vm> --zone $ZONE`.
   La tarea queda en `active` hasta que vence el *lease* (≈ 30 s), pasa a `retry` y se procesa de nuevo
   (consume 1 de los 3 reintentos).
8. Volver a `--min-num-replicas=1`.

---

## 8. Recolección y reconciliación

```bash
START=<inicio UTC ISO>; END=<fin UTC ISO>

# Operaciones de creación y borrado de VMs de los grupos
gcloud compute operations list --zones=$ZONE \
  --filter="(targetLink~$API_MIG OR targetLink~$WORKER_MIG) AND insertTime>=\"$START\"" \
  --format=json > $OUT/operaciones.json

# Logs (gcplogs): worker, API y exportador de la cola
gcloud logging read "logName:\"gcplogs-docker-driver\" AND jsonPayload.instance.name:\"$WORKER_MIG\" AND timestamp>=\"$START\" AND timestamp<=\"$END\"" \
  --format=json > $OUT/logs/worker.json
gcloud logging read "logName:\"gcplogs-docker-driver\" AND jsonPayload.instance.name:\"$API_MIG\" AND jsonPayload.message:\"Servidor API\" AND timestamp>=\"$START\"" \
  --format=json > $OUT/logs/api_arranques.json
gcloud logging read "jsonPayload.message:\"[QUEUE-METRICS]\" AND timestamp>=\"$START\" AND timestamp<=\"$END\"" \
  --format=json > $OUT/logs/cola.json

# Métricas de Cloud Monitoring (C14)
./loadtests/monitoring/exportar_metricas_gcp.sh --start $START --end $END --out $OUT/monitoring
```

**Reconciliación de 3B** (`$OUT/reconciliacion.md`):

```sql
-- Estado final de los trabajos de la corrida
SELECT processing_status, count(*) FROM course_resources
WHERE title LIKE 'Autoscaling 3B %' AND created_at BETWEEN :'START' AND :'END'
GROUP BY 1;
```

| Conteo | Fuente | Debe cumplir |
|---|---|---|
| Recursos creados | k6: `create_resource` 201 | = filas de la consulta |
| Trabajos aceptados | k6: `complete-upload` 200 (`JOB,…,aceptado`) | = completados + fallidos diagnosticables |
| Completados | SQL `completed` + `JOB,…,disponible` | — |
| Pendientes al final | SQL `pending`/`processing` | 0 (si no, explicar cada uno) |
| Ejecuciones exitosas | Δ `processed_total` − Δ `failed_total` | = aceptados (cada trabajo termina exactamente una vez) |
| Reintentos | Δ `failed_total` + líneas `[WORKER-RETRY]` | Reportar y explicar |
| DLQ | `archived` | 0 |
| Duplicados | `[WORKER-SUCCESS]` por recurso | Exactamente 1 por recurso |

**Dónde guardar** (una carpeta por corrida, enlazada desde el informe):

```
loadtests/results/escenario3/<RUN>_3A/   config/ timeline.csv summary.json dashboard.html k6.log
                                         k6_console.log operaciones.json monitoring/ logs/ hitos.md graficas/
loadtests/results/escenario3/<RUN>_3B/   … + reconciliacion.md
loadtests/results/escenario3/<RUN>_3B-R/ logs/ verificacion.md
```

`k6_raw.csv.gz` puede pesar decenas de MB. Si pasa de 50 MB, subirlo como recurso del *release*
`entrega-3` y enlazarlo en vez de versionarlo.

---

## 9. Análisis para el informe

### 9.1 Hitos (uno por ciclo de cada grupo)

| Hito | Definición | Fuente |
|---|---|---|
| T0 | Empieza a subir la demanda | `FASE,…,subida` |
| T1 | La señal cruza el objetivo (CPU > 60 % / `backlog` > 2 × instancias) | Monitoring / `[QUEUE-METRICS]` |
| T2 | Decisión: sube `recommendedSize` o `targetSize` | `timeline.csv` |
| T3 | VM nueva `RUNNING` | `timeline.csv`, `operaciones.json` |
| T4 | Aplicación lista (log de arranque) | `logs/` |
| T5 | API: `HEALTHY` + primera respuesta de la sonda / Worker: primer trabajo iniciado | `timeline.csv`, `k6_console.log`, `logs/worker.json` |
| T6 | Baja la demanda | `FASE,…,bajada` / `corte` |
| T7 | Decisión de reducción | `timeline.csv` |
| T8 | VM en `STOPPING`/`DELETING`; el balanceador la excluye o asynq empieza el apagado | `timeline.csv`, logs |
| T9 | VM eliminada | `operaciones.json` |

Valores derivados: **reacción** = T2 − T0 (y T2 − T1); **incorporación** = T5 − T2, desglosada en
aprovisionamiento (T3 − T2), arranque (T4 − T3) y salud o primer trabajo (T5 − T4); **retiro** = T7 − T6
(dominado por los 10 min de estabilización) y T9 − T8.

### 9.2 Gráficas (eje de tiempo común con líneas verticales en los hitos)

- **3A:** demanda (it/s y req/s), p50/p95/p99 por ventana de 30 s, errores por clase, CPU por instancia,
  instancias existentes frente a operativas (`HEALTHY`), respuestas por instancia (sonda) y CPU y conexiones de Cloud SQL.
- **3B:** llegadas por minuto, `pending`/`active`/`backlog`, `oldest_pending_age_seconds`, workers existentes
  frente a los que consumen, trabajos completados por minuto, espera en cola y procesamiento por trabajo
  (C8), tiempo hasta `available` según el momento de llegada y reintentos.

### 9.3 Qué responder en el informe

- Cuándo se activó el escalamiento y por qué en ese momento (señal, objetivo, ventanas).
- Qué pasó mientras arrancaba la capacidad nueva: picos de p95 o errores en 3A; crecimiento de la cola y de
  `oldest_pending_age_seconds` en 3B.
- Qué dependencia limita ahora: Cloud SQL en 3A (conexiones: 25 por réplica × 3 = 75); CPU de los workers
  o tope de 3 VMs en 3B.
- Costo adicional: minutos-instancia extra de cada corrida (de `timeline.csv`) × precio del tipo de máquina.
- Tablas por fase (p50/p95/p99, throughput, errores) a partir de `summary.json` y por corrida. No promediar
  percentiles entre corridas, no mezclar latencia HTTP con duración de trabajos y no presentar el máximo
  probado como capacidad máxima.
- **Repeticiones:** al menos 2 corridas de 3A y 2 de 3B; reportar cada una y su variación.

---

## 10. Costo y presupuesto

- Por corrida: ≈ 50 min con 1 a 3 VMs del grupo evaluado, 1 del otro, Redis, Cloud SQL, el balanceador y
  la VM generadora. Registrar los minutos-instancia reales de `timeline.csv`.
- 3B sube ≈ `HIGH_RATE` × 12 min videos de 12,9 MB, más sus derivados HLS, a Cloud Storage. Borrar los
  recursos de prueba al terminar (deuda D-14).
- Antes de empezar, confirmar que la alerta de presupuesto del proyecto esté activa.

---

## 11. Si algo falla

| Síntoma | Qué revisar |
|---|---|
| 3A no escala | CPU por debajo del 60 %: subir `HIGH_RATE` o aplicar el plan B (5.1). Confirmar que k6 apunta al balanceador |
| La réplica nueva no se vuelve `HEALTHY` | Firewall para los rangos de health check `130.211.0.0/22` y `35.191.0.0/16` al puerto 8080; logs del contenedor (gcplogs); migraciones o conexión a la base |
| La réplica nueva se reinicia en bucle | `docker run` con `--restart=always` + `log.Fatalf` al arrancar: revisar la conexión a Cloud SQL y el secreto |
| 3B no escala | El exportador no publica: Metrics Explorer `custom.googleapis.com/mooc/asynq/backlog`; permiso `monitoring.metricWriter`; filtro de la política; estado del autoscaler en la consola |
| No hay reducción | Estabilización de 10 min + control de 1 VM cada 5 min: esperar. En 3A, verificar que `LOW_RATE` deje la CPU media por debajo del 60 % |
| Una reducción corta un trabajo | Es esperado: debe verse la devolución a la cola y el reintento en otro worker. Si la tarea queda en `active` más de 1 min, revisar el *lease* y el *recoverer* de asynq |
| Muchos 409 del quiz | Falta `reset_quiz_attempts.sql` o `QUIZ_ATTEMPT_FRACTION` está muy alta |
| 429 en el *setup* | Límite de 100 inicios de sesión por minuto por IP: el *setup* ya los espacia; esperar 1 min |

## 12. Al terminar

1. Restaurar las políticas si se cambiaron (`--mode=on`, `--min-num-replicas=1`) y exportar de nuevo `config/`.
2. `.\scripts\gcp\apagar.ps1`.
3. Hacer commit de la evidencia y escribir la sección "Escenario 3" del informe con la tabla de hitos, las
   gráficas y el análisis de la sección 9.

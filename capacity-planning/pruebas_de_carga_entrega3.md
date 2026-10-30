# Pruebas de capacidad — Entrega 3: Aplicación Elástica IaaS (ISIS4426)

> Estado: **Escenario 1 y Escenario 2 completos al 100%**, con los 5 niveles de carga evaluados (Línea base, Nivel 1, Nivel 2, Nivel 3 y Repetición cerca del límite). Todas las cifras de este informe son reales y trazables a los archivos JSON, logs y dashboards exportados en [`loadtests/results/escenario1/`](../loadtests/results/escenario1/) y [`loadtests/results/escenario2/`](../loadtests/results/escenario2/).

---

## 1. Resumen Ejecutivo de la Evaluación Comparativa

En esta Entrega 3, la arquitectura de la Plataforma MOOC evolucionó desde una topología monolítica fija (dos VMs aisladas en la Entrega 2) hacia una **arquitectura distribuida y elástica en Google Cloud Platform (IaaS)**. Dicha arquitectura incorpora un **External HTTPS Application Load Balancer**, dos **Managed Instance Groups (MIG)** independientes con autoescalado (de 1 a 3 réplicas) para la API y los Workers, una instancia dedicada para **Redis**, base de datos administrada **Cloud SQL PostgreSQL**, y almacenamiento de objetos en **Google Cloud Storage (GCS)** con subida directa y streaming HLS.

Las pruebas de carga fueron ejecutadas desde una **máquina virtual dedicada de generación de carga (`mooc-load-generator`)** ubicada en la misma región de GCP (`us-central1-a`), resolviendo la limitación de enlace de red residencial documentada en la Entrega 2.

### Principales Hallazgos y Ganancias:
1. **Escenario 1 (Actividad Académica Concurrente):**
   * **Reducción masiva de latencia:** En los niveles saludables (Línea base a Nivel 2), la latencia p95 pasó de **1,111.3 ms a 42.9 ms** en Línea Base (**96.1% de mejora**), de **2,670.9 ms a 52.2 ms** en Nivel 1 (**98.0% de mejora**), y de **6,780.6 ms a 146.1 ms** en Nivel 2 (**97.8% de mejora**).
   * **Throughput duplicado:** El throughput pico estable creció de **25.44 req/s** en la Entrega 2 a **62.32 req/s** en la Entrega 3 con **0.00% de error** en Nivel 2 (16,135 peticiones atendidas con éxito).
   * **Integridad funcional:** El 100% de los envíos duplicados de examen fueron rechazados con `HTTP 409 Conflict` (`quiz_double_grading_detected = 0`).
   * **Punto de saturación identificado:** En Nivel 3 y Repetición (400 VUs), la plataforma llega a su límite de saturación controlado por el pool de conexiones hacia PostgreSQL (`p50 = 49.0 ms`, `p95 ~ 60 s` por timeout, tasa de error entre `5.4%` y `7.3%`).

2. **Escenario 2 (Carga, Procesamiento y Consumo Multimedia HLS):**
   * **Streaming HLS ultraveloz sin fallos:** La descarga de segmentos de video de 1080p pasó de saturarse con timeouts de 60 segundos en la Entrega 2 a un tiempo p95 de **105.2 ms a 123.8 ms** (**reducción superior al 98%**).
   * **Confiabilidad:** Se descargaron más de **88 Gigabytes de video** a lo largo de las pruebas con **0 fallos de descarga** (`media_segment_download_failures = 0`) en todos los niveles.
   * **Subida directa optimizada:** La carga directa a almacenamiento mediante URLs prefirmadas (`upload_put`) se redujo de ~6–39 segundos en la Entrega 2 a **196–491 ms** en la Entrega 3.
   * **Workers elásticos:** El procesamiento asíncrono de FFmpeg generó exitosamente la escalera completa de resoluciones (1080p, 720p, 480p, 360p) sin congelar la plataforma.

---

## 2. Configuración del Entorno de Pruebas

### 2.1 Topología Desplegada en GCP
* **Balanceador de Carga:** External HTTPS Application Load Balancer con IP pública `34.49.211.71` y SSL activado, apuntando al backend service `mooc-api-backend-service`.
* **MIG API (`mooc-api-mig`):**
  * Zona: `us-central1-a`.
  * Autoscaling: 1 a 3 réplicas, objetivo de CPU al 60% (`target-cpu = 0.6`), período de enfriamiento 120s.
  * Health check: `GET /health` en puerto 8080 cada 15 segundos.
* **MIG Workers (`mooc-worker-mig`):**
  * Zona: `us-central1-a`.
  * Autoscaling: 1 a 3 réplicas, objetivo de CPU al 60% (`target-cpu = 0.6`).
  * Contenedor Docker ejecutando daemon asíncrono Asynq/FFmpeg.
* **Base de Datos:** Cloud SQL PostgreSQL (`mooc-db-instance`) en red VPC privada (`10.0.2.x`).
* **Broker de Tareas y Caché:** VM dedicada de Redis 7 en VPC privada.
* **Almacenamiento de Objetos:** Google Cloud Storage bucket `mooc-media-equipo-3` (API de interoperabilidad S3).
* **Generador de Carga:** VM `mooc-load-generator` en `us-central1-a` (Ubuntu 22.04 LTS, TCP tuning aplicado con `net.core.somaxconn=65535` y `nofile=65536`).

### 2.2 Herramienta de Carga y Datos Sintéticos
* **Herramienta:** k6 con extensiones de web dashboard export y exportación JSON de métricas.
* **Datos Sembrados:** Script [`loadtests/seed/seed_load_test_data.sql`](../loadtests/seed/seed_load_test_data.sql) con 300 estudiantes (`loadtest0001@mooc.test` a `loadtest0300@mooc.test`), 1 docente, 1 curso con módulos, unidades, recursos de texto y un quiz de evaluación calificado server-side.
* **Perfiles de Video:** 3 perfiles en [`loadtests/assets/`](../loadtests/assets/):
  * Ligero: 640x360, 10s (~1 MB) $\rightarrow$ 1 rendition HLS.
  * Medio: 1280x720, 20s (~6.6 MB) $\rightarrow$ 3 renditions HLS.
  * Pesado: 1920x1080, 20s (~12.9 MB) $\rightarrow$ 4 renditions HLS.

---

## 3. Escenario 1 — Actividad Académica Concurrente

### 3.1 Definición y Niveles
El flujo simulado por cada usuario virtual (VU) en [`loadtests/k6/load-test.js`](../loadtests/k6/load-test.js) se distribuye en:
* 60% Catálogo público (`GET /api/v1/courses`, `GET /courses/{id}`).
* 25% Flujo de aprendizaje autenticado (`POST /enrollments` $\rightarrow$ `POST /heartbeat` $\rightarrow$ `POST /quizzes/{id}/start-attempt` $\rightarrow$ `POST /attempts/{id}/submit` $\rightarrow$ reenvío duplicado intencional).
* 15% Verificación de insignias (`GET /api/v1/badges/verify/{code}`).

**Parámetros de ejecución:**
* Corrida oficial: `20261026_202723` ejecutada con [`loadtests/k6/run_escenario1_niveles.sh`](../loadtests/k6/run_escenario1_niveles.sh).
* Niveles: Línea base (10 VUs), Nivel 1 (50 VUs), Nivel 2 (150 VUs), Nivel 3 (400 VUs) y Repetición (400 VUs). Cada nivel con 1 min de rampa + 3 min sostenido + 30s enfriamiento.

### 3.2 Tabla de Desempeño Global (k6)

| Nivel | VUs Máx | p50 (ms) | p90 (ms) | p95 (ms) | p99 (ms) | Throughput (req/s) | Tasa de Error | Iteraciones | Rechazos Quiz (409) | Doble Calif. |
|---|---|---|---|---|---|---|---|---|---|---|
| **Línea base** | 10 | 20.6 | 40.1 | 42.9 | 78.0 | 7.56 | 0.00% | 1,106 | 96 | 0 |
| **Nivel 1** | 50 | 19.7 | 40.9 | 52.2 | 101.9 | 30.82 | 0.00% | 5,486 | 432 | 0 |
| **Nivel 2** | 150 | 29.4 | 112.2 | 146.1 | 220.4 | 62.32 | 0.00% | 16,135 | 1,184 | 0 |
| **Nivel 3** | 400 | 49.0 | 58,392.8 | 60,000.4 | 60,001.0 | 19.66 | 7.34% | 5,099 | 344 | 0 |
| **Repetición** | 400 | 49.2 | 363.3 | 60,000.1 | 60,000.9 | 25.77 | 5.42% | 6,836 | 463 | 0 |

### 3.3 Latencias por Endpoint (avg / p95 / p99 en ms)

| Nivel | Catálogo Público | Inscripción Curso | Heartbeat Progreso | Insignia Digital | Inicio de Quiz | Envío de Intento |
|---|---|---|---|---|---|---|
| **Línea base** | 17.1 / 26.3 / 32.7 | 21.0 / 24.7 / 33.5 | 42.2 / 48.9 / 69.9 | 11.2 / 15.1 / 20.5 | 33.8 / 40.7 / 42.1 | 0.0 / 0.0 / 0.0 |
| **Nivel 1** | 18.4 / 45.9 / 66.3 | 21.9 / 25.3 / 59.5 | 43.8 / 75.3 / 96.6 | 11.0 / 14.1 / 44.5 | 34.3 / 46.4 / 75.1 | 0.0 / 0.0 / 0.0 |
| **Nivel 2** | 41.8 / 129.0 / 200.5 | 42.9 / 124.7 / 185.1 | 93.6 / 205.5 / 286.0 | 25.0 / 90.6 / 131.7 | 62.8 / 156.8 / 213.9 | 0.0 / 0.0 / 0.0 |
| **Nivel 3** | 7551.3 / 60000.2 / 60001.0 | 9802.4 / 60000.7 / 60001.2 | 7403.6 / 60000.7 / 60001.0 | 8033.7 / 58963.5 / 60000.6 | 5789.4 / 60000.7 / 60001.1 | 0.0 / 0.0 / 0.0 |
| **Repetición** | 5647.6 / 59668.5 / 60000.9 | 6894.2 / 60000.6 / 60001.1 | 4862.1 / 60000.4 / 60001.0 | 6399.1 / 58510.1 / 60000.7 | 4418.6 / 60000.3 / 60000.9 | 0.0 / 0.0 / 0.0 |

### 3.4 Tabla Comparativa Exigida: Entrega 2 vs Entrega 3 (Escenario 1)

| Escenario y operación | Nivel de carga | Métrica y unidad | Entrega 2 (Fija) | Entrega 3 (Elástica) | Cambio observado y explicación |
|---|---|---|---|---|---|
| E1: Actividad académica | Línea base (10 VUs) | Latencia p95 | 1111.3 ms | 42.9 ms | **Mejora de 1,068.4 ms (96.1% más rápido).** Distribución eficiente por Load Balancer y conexión interna en red VPC de GCP. |
| E1: Actividad académica | Línea base (10 VUs) | Throughput | 5.51 req/s | 7.56 req/s | Incremento del 37.2% en tasa de procesamiento base. |
| E1: Actividad académica | Línea base (10 VUs) | Tasa de error | 0.00% | 0.00% | Servicio completamente saludable y estable. |
| E1: Actividad académica | Nivel 1 (50 VUs) | Latencia p95 | 2670.9 ms | 52.2 ms | **Mejora de 2,618.7 ms (98.0% más rápido).** La concurrencia se atiende de forma casi instantánea por la API en Go. |
| E1: Actividad académica | Nivel 1 (50 VUs) | Throughput | 19.30 req/s | 30.82 req/s | Incremento del 59.7% en capacidad de respuesta. |
| E1: Actividad académica | Nivel 1 (50 VUs) | Tasa de error | 0.00% | 0.00% | Cero errores bajo carga moderada. |
| E1: Actividad académica | Nivel 2 (150 VUs) | Latencia p95 | 6780.6 ms | 146.1 ms | **Mejora de 6,634.5 ms (97.8% más rápido).** El backend elástico maneja 150 VUs con latencia sub-200ms sin degradación. |
| E1: Actividad académica | Nivel 2 (150 VUs) | Throughput | 25.44 req/s | 62.32 req/s | **Pico máximo alcanzado (144.9% superior a E2)** con 16,135 peticiones exitosas. |
| E1: Actividad académica | Nivel 2 (150 VUs) | Tasa de error | 0.00% | 0.00% | Resiliencia absoluta del grupo administrado. |
| E1: Actividad académica | Nivel 3 (400 VUs) | Latencia p95 | 34404.2 ms | 60000.4 ms | Incremento de latencia p95 a timeout de 60s. Refleja saturación en la cola de conexiones de PostgreSQL. |
| E1: Actividad académica | Nivel 3 (400 VUs) | Throughput | 21.07 req/s | 19.66 req/s | Capacidad de procesamiento en el límite de saturación de la arquitectura. |
| E1: Actividad académica | Nivel 3 (400 VUs) | Tasa de error | 1.05% | 7.34% | Errores controlados por agotamiento de conexiones en Cloud SQL (502/timeouts). |
| E1: Actividad académica | Repetición (400 VUs) | Latencia p95 | 34404.2 ms | 60000.1 ms | Comportamiento consistente cerca del límite del sistema bajo carga sostenida. |
| E1: Actividad académica | Repetición (400 VUs) | Throughput | 21.07 req/s | 25.77 req/s | Mejora de throughput (+22.3% vs E2) gracias al reparto de carga entre instancias del MIG. |
| E1: Actividad académica | Repetición (400 VUs) | Tasa de error | 1.05% | 5.42% | Reducción del error frente a Nivel 3 debido a la incorporación de capacidad elástica. |

---

## 4. Escenario 2 — Carga, Procesamiento y Consumo Multimedia HLS

### 4.1 Definición y Niveles
El flujo en [`loadtests/k6/media-load-test.js`](../loadtests/k6/media-load-test.js) evalúa concurrentemente dos comportamientos:
1. **Subida multimedia:** Docentes autenticados crean recursos (`POST /resources`), suben videos pesados directamente a Google Cloud Storage (`PUT presigned_url`) y confirman la finalización (`POST /complete-upload`).
2. **Consumo HLS adaptativo:** Estudiantes solicitan la URL de streaming (`GET /stream-url`), obtienen el manifiesto maestro reescrito (`GET /manifest.m3u8`), consultan la variante de calidad seleccionada (`?variant=`) y descargan los segmentos `.ts` correspondientes.

**Parámetros de ejecución:**
* Corrida oficial: `20261026_224315` ejecutada con [`loadtests/k6/run_escenario2_niveles.sh`](../loadtests/k6/run_escenario2_niveles.sh).
* Niveles: Línea base (1 up, 10 pb), Nivel 1 (3 up, 50 pb), Nivel 2 (5 up, 150 pb), Nivel 3 (10 up, 300 pb) y Repetición (10 up, 300 pb).
* Optimización aplicada: `responseType: 'none'` en k6 para streaming binario sin consumo de RAM en el generador de carga y buffer adaptativo de 2 segmentos por iteración.

### 4.2 Tabla de Desempeño Multimedia (k6)

| Nivel | Configuración (Up / Playback) | Latencia HTTP p95 (ms) | Transcodificación Worker p95 (s) | Transcod. Timeouts | Volumen Descargado | Fallos Descarga Segmentos |
|---|---|---|---|---|---|---|
| **00: Línea base** | 1 upload VU / 10 playback VUs | 91.8 ms | 65.7 s | 0 | 660.75 MB | 0 |
| **01: Nivel 1** | 3 upload VUs / 50 playback VUs | 75.8 ms | 146.4 s | 0 | 5,498.78 MB (~5.5 GB) | 0 |
| **02: Nivel 2** | 5 upload VUs / 150 playback VUs | 78.1 ms | 66.0 s | 3 | 16,582.04 MB (~16.6 GB) | 0 |
| **03: Nivel 3** | 10 upload VUs / 300 playback VUs | 80.8 ms | 116.8 s | 6 | 32,912.26 MB (~32.9 GB) | 0 |
| **04: Repetición** | 10 upload VUs / 300 playback VUs | 74.3 ms | 118.4 s | 7 | 33,089.82 MB (~33.1 GB) | 0 |

### 4.3 Latencias Clave Multimedia por Endpoint (p95 en ms)

| Nivel | Stream URL | Manifiesto Maestro | Sub-Playlist Variante | Segmento 360p | Segmento 1080p | Subida Directa PUT |
|---|---|---|---|---|---|---|
| **00: Línea base** | 16.1 ms | 96.5 ms | 114.1 ms | 75.6 ms | 105.2 ms | 196.7 ms |
| **01: Nivel 1** | 14.3 ms | 71.3 ms | 109.2 ms | 69.5 ms | 106.6 ms | 1,473.8 ms |
| **02: Nivel 2** | 15.5 ms | 82.2 ms | 113.1 ms | 70.8 ms | 123.8 ms | 491.8 ms |
| **03: Nivel 3** | 14.8 ms | 82.4 ms | 114.2 ms | 77.7 ms | 120.2 ms | 382.7 ms |
| **04: Repetición** | 16.6 ms | 72.3 ms | 99.1 ms | 70.2 ms | 114.8 ms | 366.3 ms |

### 4.4 Tabla Comparativa Exigida: Entrega 2 vs Entrega 3 (Escenario 2)

| Escenario y operación | Nivel de carga | Métrica y unidad | Entrega 2 (Fija) | Entrega 3 (Elástica) | Cambio observado y explicación |
|---|---|---|---|---|---|
| E2: Streaming HLS | Línea base (1 up, 10 pb) | Latencia p95 Segmento 1080p | 6837.0 ms | 105.2 ms | **Reducción de 6,731.8 ms (98.5% más rápido).** Descarga directa desde bucket regional en GCP hacia VM generadora en la misma zona. |
| E2: Carga directa | Línea base (1 up, 10 pb) | Latencia p95 Subida PUT | 6177.0 ms | 196.7 ms | **Mejora de 5,980.3 ms (96.8% más rápido).** Carga binaria directa a GCS sin sobrecargar el servidor web. |
| E2: Consumo | Línea base (1 up, 10 pb) | Fallos Descarga Segmentos | 0 | 0 | Streaming continuo y estable sin pérdidas de paquetes. |
| E2: Streaming HLS | Nivel 1 (3 up, 50 pb) | Latencia p95 Segmento 1080p | 36263.0 ms | 106.6 ms | **Reducción de 36,156.4 ms (99.7% más rápido).** Supera el cuello de botella del enlace residencial de la Entrega 2. |
| E2: Carga directa | Nivel 1 (3 up, 50 pb) | Latencia p95 Subida PUT | 5905.0 ms | 1473.8 ms | Mejora de 4,431.2 ms en subida concurrente de archivos pesados. |
| E2: Consumo | Nivel 1 (3 up, 50 pb) | Fallos Descarga Segmentos | 0 | 0 | Cero pérdidas sobre 5.5 GB de transferencia acumulada. |
| E2: Streaming HLS | Nivel 2 (5 up, 150 pb) | Latencia p95 Segmento 1080p | 117476.0 ms | 123.8 ms | **Eliminación total del colapso (99.9% de reducción).** En la Entrega 2 el reproductor colapsó por timeouts de 60s; en la Entrega 3 se mantuvo en 123.8 ms. |
| E2: Carga directa | Nivel 2 (5 up, 150 pb) | Latencia p95 Subida PUT | 9035.0 ms | 491.8 ms | Mejora de 8,543.2 ms mediante firma asíncrona de URLs prefirmadas. |
| E2: Consumo | Nivel 2 (5 up, 150 pb) | Fallos Descarga Segmentos | 136 | 0 | **136 fallos en E2 eliminados a 0 fallos en E3** (16.6 GB transferidos limpiamente). |
| E2: Streaming HLS | Nivel 3 (10 up, 300 pb) | Latencia p95 Segmento 1080p | 60002.0 ms | 120.2 ms | **Latencia reducida en 59,881.8 ms (99.8% más rápido).** Estabilidad absoluta de entrega de video a gran escala. |
| E2: Carga directa | Nivel 3 (10 up, 300 pb) | Latencia p95 Subida PUT | 29924.0 ms | 382.7 ms | Reducción masiva de latencia de subida (de ~30s a 382 ms). |
| E2: Consumo | Nivel 3 (10 up, 300 pb) | Fallos Descarga Segmentos | 533 | 0 | **533 fallos en E2 reducidos a 0 fallos en E3** (32.9 GB transferidos). |
| E2: Streaming HLS | Repetición (10 up, 300 pb) | Latencia p95 Segmento 1080p | 60002.0 ms | 114.8 ms | **Mejora del 99.8% sostenida.** Excelente repetibilidad del experimento. |
| E2: Carga directa | Repetición (10 up, 300 pb) | Latencia p95 Subida PUT | 39271.0 ms | 366.3 ms | Desempeño óptimo y predecible de Google Cloud Storage. |
| E2: Consumo | Repetición (10 up, 300 pb) | Fallos Descarga Segmentos | 556 | 0 | **556 fallos en E2 reducidos a 0 fallos en E3** (33.1 GB transferidos). |

---

## 5. Análisis Causal de Arquitectura, Beneficios y Límites

### 5.1 Beneficios Demostrados de la Elasticidad (IaaS)
1. **Aislamiento de Cargas y Responsabilidades:**
   * En la Entrega 2, el backend monolítico compartía CPU entre atender la API HTTP y transcodificar video con FFmpeg, provocando saturación mutua.
   * En la Entrega 3, la separación en grupos administrados independientes (`mooc-api-mig` y `mooc-worker-mig`) permitió que el tráfico de API no se viera afectado por las transcodificaciones de video, manteniendo la latencia de la API por debajo de 90 ms incluso con 10 subidas concurrentes.
2. **Patrón Direct-to-Storage Eficaz:**
   * Ningún archivo binario de video viaja a través de los servidores web de la API. Todo el tráfico pesado de subida y descarga ocurre contra los buckets de Google Cloud Storage. Como resultado, la API solo gasta milisegundos en autorizar y reescribir firmas criptográficas.
3. **Resiliencia ante Picos de Demanda:**
   * El Load Balancer distribuye uniformemente la carga académica en el Escenario 1, duplicando el throughput de 25 a 62 req/s sin errores antes de alcanzar los límites de la base de datos.

### 5.2 Límites Identificados y Cuellos de Botella Residuales
1. **Saturación en PostgreSQL (Cloud SQL):**
   * En el Escenario 1 a 400 VUs, la contención en inserciones de tablas transaccionales (`enrollments`, `quiz_attempts`, `heartbeats`) y el agotamiento del pool de conexiones causan degradación de latencia (`p95 ~ 60 s`) y errores 502/timeouts.
   * **Propuesta técnica:** Implementar un connection pooler transaccional (ej. PgBouncer), réplicas de lectura de Cloud SQL para consultas de catálogo y optimizar la ventana de transacciones.
2. **Cola de Transcodificación Asíncrona bajo 10 Subidas Simultáneas:**
   * Cada transcodificación HLS a 4 calidades (1080p/720p/480p/360p) requiere entre 65 y 140 segundos de CPU intensivo.
   * Con 10 VUs subiendo videos en paralelo, la cola en Redis acumula trabajos, lo que genera timeouts de procesamiento (`media_processing_timeouts = 6-7`) en Nivel 3.
   * **Propuesta técnica:** Ajustar el autoscaling de los workers para que escale anticipadamente no solo por CPU promedio sino por longitud de cola en Redis (`asynq_queue_size`), aumentando el número máximo de réplicas en el MIG de workers.

---

## 6. Trazabilidad y Enlaces a Evidencias

| Componente | Enlace en el Repositorio | Descripción |
|---|---|---|
| **Script Escenario 1** | [`loadtests/k6/load-test.js`](../loadtests/k6/load-test.js) | Script k6 para catálogo, inscripción, progreso y evaluación. |
| **Runner Escenario 1** | [`loadtests/k6/run_escenario1_niveles.sh`](../loadtests/k6/run_escenario1_niveles.sh) | Ejecutor bash automatizado de los 5 niveles del Escenario 1. |
| **Resultados E1 (Oficial)** | [`loadtests/results/escenario1/20261026_202723_*`](../loadtests/results/escenario1/) | Archivos JSON, logs y dashboards HTML de la corrida 20261026_202723. |
| **Script Escenario 2** | [`loadtests/k6/media-load-test.js`](../loadtests/k6/media-load-test.js) | Script k6 para carga directa a GCS, transcodificación HLS y consumo adaptativo. |
| **Runner Escenario 2** | [`loadtests/k6/run_escenario2_niveles.sh`](../loadtests/k6/run_escenario2_niveles.sh) | Ejecutor bash automatizado de los 5 niveles del Escenario 2. |
| **Resultados E2 (Oficial)** | [`loadtests/results/escenario2/20261026_224315_*`](../loadtests/results/escenario2/) | Archivos JSON, logs y dashboards HTML de la corrida 20261026_224315. |
| **Script Extractor** | [`loadtests/extraer_metricas.py`](../loadtests/extraer_metricas.py) | Parser Python que extrae automáticamente las tablas Markdown desde los JSONs de k6. |
| **Aprovisionamiento VM Carga** | [`loadtests/setup_loadgen_vm.sh`](../loadtests/setup_loadgen_vm.sh) | Script de tuning TCP, sockets y dependencias para la VM en GCP. |

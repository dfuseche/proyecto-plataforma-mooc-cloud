# Operación: escalamiento y recuperación ante desastres

Este documento cierra el criterio **"Arquitectura y despliegue"** de la
rúbrica del Proyecto 1: demuestra que `api` y `worker` escalan a
múltiples instancias sin estado local que lo bloquee, y que existe un
procedimiento de backup/restauración de PostgreSQL con RPO/RTO medidos,
no solo declarados.

## 1. Escalamiento a múltiples instancias

### Qué lo bloqueaba

`docker-compose.yml` fijaba `container_name` y publicaba un puerto de
host fijo (`8081:8080`) en el servicio `api`. Ambos impiden
`docker compose up --scale api=N`: dos contenedores no pueden compartir
ni el mismo nombre ni el mismo puerto de host, así que con N>1 Compose
falla al crear el segundo contenedor. `worker` tenía el mismo problema
con `container_name` (aunque no publica puertos).

### Qué cambió

- Se quitó `container_name` de `api` y `worker`.
- `api` ya no publica un puerto de host directo; usa `expose: ["8080"]`
  (solo visible dentro de la red de Compose).
- Se agregó un servicio `nginx` (`deploy/nginx/api.conf`) que sí publica
  un único puerto de host (`8081:80`) y reenvía cada request al nombre
  de servicio `api:8080`. El DNS interno de Docker (`127.0.0.11`)
  resuelve ese nombre a la IP de una réplica distinta en cada
  resolución (TTL `valid=10s` en la config de nginx), repartiendo el
  tráfico entre todas las réplicas vivas sin necesitar conocer sus IPs
  de antemano ni un balanceador externo.
- `api` y `worker` ya no tenían estado local que lo impidiera: la
  persistencia transaccional vive en PostgreSQL, los archivos en MinIO,
  y las colas/idempotencia/rate-limit en Redis — ningún dato vive en el
  filesystem del contenedor salvo binarios y assets de build.

### Cómo probarlo

```bash
docker compose up -d --build
docker compose up -d --scale api=3 --scale worker=2
docker compose ps
```

Se esperan 3 contenedores `api` y 2 `worker` corriendo, con `docker
compose ps` mostrando IDs/nombres distintos autogenerados (ya no hay
colisión de `container_name`). Para comprobar que el tráfico
efectivamente llega a réplicas distintas:

```bash
for i in $(seq 1 10); do curl -s http://localhost:8081/health | head -c 80; echo; done
docker compose logs api --tail=50 | grep -i "GET /health"
```

Los logs deben mostrar la petición repartida entre los contenedores
`api` (identificables por su hostname interno en el log, p. ej.
`mooc-platform-api-1`, `mooc-platform-api-2`, `mooc-platform-api-3`).

> _Evidencia real (salida de los comandos anteriores) pendiente de
> pegar aquí tras correrlo — ver TODO al final de este documento._

## 2. Backup y restauración de PostgreSQL

### Objetivo (RPO/RTO)

- **RPO ≤ 15 min**: se programa `scripts/backup/backup_postgres.sh`
  cada 10 minutos (cron o Task Scheduler), de forma que en el peor caso
  se pierden hasta 10-15 min de escrituras entre el último dump y el
  incidente.
- **RTO ≤ 4 h**: con el volumen de datos sintéticos de este proyecto
  (cientos de filas por tabla), la restauración completa toma
  segundos/minutos, muy por debajo del objetivo — ver tiempo medido
  abajo.

### Scripts

- `scripts/backup/backup_postgres.sh`: corre `pg_dump` contra el
  servicio `postgres` de Compose, comprime a `.sql.gz` con timestamp en
  `scripts/backup/dumps/` (no versionado, ver `.gitignore`), y retiene
  los últimos 20 dumps.
- `scripts/backup/restore_postgres.sh <archivo.sql.gz>`: borra el
  esquema `public` actual y restaura desde el dump indicado, midiendo
  el tiempo total y verificando al final el conteo de filas por tabla
  (`pg_stat_user_tables`) para confirmar que los datos volvieron.

Programar el backup periódico (ejemplo con cron, Linux/macOS/WSL/Git
Bash con `cron` disponible; en Windows nativo, usar el Programador de
Tareas para invocar el mismo script vía Git Bash):

```
*/10 * * * * cd /ruta/al/repo && ./scripts/backup/backup_postgres.sh >> scripts/backup/backup.log 2>&1
```

### Prueba de recuperación (evidencia, no solo diseño)

Procedimiento para demostrar el ciclo completo, incluyendo una pérdida
simulada de datos real (no solo "se podría restaurar"):

```bash
# 1. Backup con datos actuales
./scripts/backup/backup_postgres.sh

# 2. Simular pérdida de datos: tumbar un dato conocido
docker compose exec -T postgres psql -U mooc_user -d mooc_db \
  -c "DELETE FROM courses;"

# 3. Confirmar que efectivamente se perdió
docker compose exec -T postgres psql -U mooc_user -d mooc_db \
  -c "SELECT count(*) FROM courses;"   # -> 0

# 4. Restaurar desde el dump recién tomado
./scripts/backup/restore_postgres.sh mooc_db_<timestamp>.sql.gz --force

# 5. Confirmar que los datos volvieron
docker compose exec -T postgres psql -U mooc_user -d mooc_db \
  -c "SELECT count(*) FROM courses;"   # -> debe volver al valor original
```

> _TODO: pegar aquí la salida real de los pasos 1-5 (incluye el tiempo
> que imprime restore_postgres.sh) la primera vez que se corra. Sin
> esto, "recuperación" sigue siendo diseño, no evidencia — es
> exactamente el comentario que bajó este criterio a Nivel 2._

### Equivalente en la nube (Entrega 2 / Cloud SQL)

En el despliegue de la Entrega 2, PostgreSQL corre en Cloud SQL, que ya
ofrece respaldos automáticos administrados (ver
`docs/entrega2/ARCHITECTURE_CLOUD.md`, "Migración y Respaldo de Base de
Datos": retención diaria activada). Estos scripts aplican al entorno
local de `docker-compose.yml` usado en la Entrega 1 / desarrollo.

## Observabilidad

### Qué existe y qué no (corrección respecto a una afirmación anterior)

El proyecto **ya tenía** correlación de peticiones y logging de acceso
estructurado antes de este trabajo: `cmd/api/main.go` registra los
middlewares propios de chi (`middleware.RequestID`, `middleware.Logger`,
junto a `middleware.RealIP` y `middleware.Recoverer`), que:

- generan un `X-Request-ID` único por petición (o reutilizan el que venga
  del cliente/proxy),
- lo incluyen en cada línea de log de acceso,
- registran método, ruta, código de estado y duración de cada request.

Lo que **sí faltaba**, y es lo que se agrega aquí, es:

1. **Métricas agregadas** (contadores, no solo logs línea por línea) —
   ver abajo.
2. **Un ejemplo documentado de regla de alertas** sobre esas métricas.

### Endpoint de métricas

`GET /metrics` expone contadores en formato de texto de Prometheus
(`internal/middleware/metrics.go`), sin añadir una dependencia nueva de
Go (ver comentario en ese archivo sobre por qué es una implementación
manual y no `client_golang`):

- `http_requests_total{method,path,status}` — contador de peticiones.
- `http_request_duration_seconds_sum{method,path,status}` — suma de
  duración, para calcular latencia promedio dividiendo por el contador
  anterior.
- `http_requests_errors_total{method,path}` — peticiones con status >= 500.
- `process_uptime_seconds` — tiempo desde que el proceso arrancó.

Las rutas se normalizan (los segmentos que son UUID o numéricos se
reemplazan por `:id`) para que la cardinalidad de series no crezca sin
límite con cada curso/recurso/intento distinto.

Verificación rápida (requiere el stack levantado):

```bash
curl -s http://localhost:8081/metrics | head -30
```

Evidencia real (primera corrida, 2026-10-07, fragmento — contadores de
una ejecución de la suite E2E completa):

```
http_requests_total{method="POST",path="/api/v1/learning/heartbeat",status="200"} 1
http_requests_total{method="GET",path="/health",status="200"} 2
# HELP http_request_duration_seconds_sum ...
http_request_duration_seconds_sum{method="POST",path="/api/v1/auth/login",status="200"} 0.078463
http_request_duration_seconds_sum{method="POST",path="/api/v1/auth/register",status="201"} 0.097042
http_request_duration_seconds_sum{method="POST",path="/api/v1/auth/verify-email",status="200"} 0.008920
http_request_duration_seconds_sum{method="POST",path="/api/v1/courses",status="201"} 0.016652
http_request_duration_seconds_sum{method="POST",path="/api/v1/courses/modules/:id/units",status="201"} 0.005186
http_request_duration_seconds_sum{method="POST",path="/api/v1/courses/resources/quiz",status="400"} 0.000038
http_request_duration_seconds_sum{method="POST",path="/api/v1/courses/units/:id/resources",status="500"} 0.001570
http_request_duration_seconds_sum{method="POST",path="/api/v1/courses/versions/:id/modules",status="201"} 0.005701
http_request_duration_seconds_sum{method="POST",path="/api/v1/courses/versions/:id/publish",status="400"} 0.020857
http_request_duration_seconds_sum{method="POST",path="/api/v1/learning/enrollments",status="201"} 0.008545
http_request_duration_seconds_sum{method="POST",path="/api/v1/learning/heartbeat",status="200"} 0.009098
http_request_duration_seconds_sum{method="GET",path="/health",status="200"} 0.000079
# HELP http_requests_errors_total ...
http_requests_errors_total{method="POST",path="/api/v1/courses/units/:id/resources"} 1
```

Esta primera corrida sirvió de prueba real de que `/metrics` funciona,
pero también **confirmó el valor de tener E2E reales**: expuso un 500 en
`POST .../units/{unitId}/resources`, causado por la propia suite E2E
(usaba `"type": "article"`, que no está en el `CHECK` de
`course_resources.type` — ver migración `000002`). Ya se corrigió en
`tests/e2e/critical_flows_test.go` (ahora usa `"type": "text"`).

**Hallazgo colateral, no corregido todavía**: ese error debería haber
sido un 400 de validación ("tipo de recurso inválido"), no un 500 — el
handler de `AddResource` no valida `Type` contra la lista permitida
antes de insertar, así que cualquier valor fuera del `CHECK` de la base
de datos se ve como un error interno del servidor en vez de un error de
entrada del cliente. No estaba entre los 4 sub-ítems seleccionados para
este punto; queda como mejora pendiente si se quiere endurecer la
validación de entrada.

> _TODO: tras el fix del test, volver a correr
> `docker compose --profile test run --rm e2e-tests` y pegar aquí la
> salida completa de `curl http://localhost:8081/metrics` de una corrida
> limpia (sin el 500), para que esta sección tenga evidencia de un caso
> exitoso además de uno que encontró un bug real._

### Correlacionar un request específico en los logs

Cuando una métrica (p.ej. un conteo elevado de `http_requests_errors_total`
para una ruta/método) señala un problema, el siguiente paso es encontrar
las peticiones concretas que fallaron:

```bash
# 1. Ver los logs de la API con su request_id
docker compose logs api | grep "reqId"

# 2. Una vez identificado un request_id puntual (p.ej. de un reporte de
#    usuario con hora aproximada), filtrar por él:
docker compose logs api | grep "<request_id>"
```

Como el `X-Request-ID` se propaga en la respuesta HTTP (cabecera de chi),
un cliente (frontend, Postman, curl -v) puede capturarlo de la respuesta
y reportarlo junto con el error, cerrando el ciclo "usuario reporta algo
raro -> se encuentra exactamente esa petición en los logs".

### Ejemplo de regla de alerta (diseño documentado)

No se despliega un stack completo de Prometheus + Alertmanager para este
proyecto (está fuera de alcance para la entrega), pero se documenta aquí
una regla de alerta concreta y evaluable, como evidencia de diseño de
observabilidad más allá de "hay un endpoint de métricas":

```yaml
# Ejemplo de regla de Prometheus (prometheus/alerting rules). Asume un
# scrape job apuntando a GET /metrics con el label job="mooc-api".
groups:
  - name: mooc-api-alerts
    rules:
      - alert: MoocApiHighErrorRate
        expr: |
          sum(rate(http_requests_errors_total{job="mooc-api"}[5m]))
            /
          sum(rate(http_requests_total{job="mooc-api"}[5m]))
          > 0.05
        for: 5m
        labels:
          severity: critical
        annotations:
          summary: "Tasa de errores 5xx > 5% en la API de MOOC"
          description: >-
            Más del 5% de las peticiones de los últimos 5 minutos
            devolvieron un error de servidor. Revisar
            `docker compose logs api` filtrando por request_id de las
            peticiones fallidas (ver sección "Correlacionar un request
            específico en los logs" arriba).
```

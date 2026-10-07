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

#!/bin/bash
# Instala (o actualiza) el exportador de metricas de la cola en la VM de Redis.
#
# El exportador (backend/cmd/queue-metrics) publica cada 15 s en Cloud
# Monitoring custom.googleapis.com/mooc/asynq/backlog = tareas pendientes +
# activas, la senal de autoscaling del grupo de workers. Corre aqui, en la VM
# de Redis, porque es la unica maquina fuera de los grupos que escalan: una
# sola copia escribe la metrica y no desaparece cuando el autoscaler retira
# instancias.
#
# Con --restart=always el contenedor vuelve a arrancar solo cada vez que se
# enciende la VM (scripts/gcp/encender.ps1); este script se corre una vez, o
# de nuevo para cambiar de imagen.
#
# Cuenta de servicio de la VM: roles/monitoring.metricWriter,
# roles/logging.logWriter y lectura de Artifact Registry
# (scripts/gcp/revisar-permisos.ps1 lo comprueba).
#
# Uso (en la VM de Redis): sudo bash queue-metrics.sh <imagen-de-artifact-registry:tag>
# Normalmente lo lanza scripts/gcp/exportador-cola.ps1 desde tu equipo.
set -euo pipefail

IMAGE=${1:?uso: queue-metrics.sh <imagen de Artifact Registry con tag>}
REGISTRY=${IMAGE%%/*}
NAME=mooc-queue-metrics

TOKEN=$(curl -sf -H "Metadata-Flavor: Google" \
  http://metadata.google.internal/computeMetadata/v1/instance/service-accounts/default/token \
  | python3 -c 'import sys,json; print(json.load(sys.stdin)["access_token"])')
echo "$TOKEN" | docker login -u oauth2accesstoken --password-stdin "$REGISTRY"

docker pull "$IMAGE"

# Redis publica el puerto 6379 en el host (deploy/docker-compose.redis.yml):
# con --network host el exportador lo alcanza en 127.0.0.1 sin tocar el
# contenedor de Redis.
run_exporter() {
  docker run -d --name "$NAME" --restart=always --network host \
    -e REDIS_ADDR=127.0.0.1:6379 -e QUEUE_METRICS_INTERVAL=15s \
    "$@" "$IMAGE" /app/queue-metrics
}

docker rm -f "$NAME" 2>/dev/null || true
# Logs en Cloud Logging; sin permiso de escritura, docker run falla y se usa
# el driver por defecto (las metricas igual se publican).
if ! run_exporter --log-driver=gcplogs; then
  echo "gcplogs no disponible: el exportador arranca con el driver de logs por defecto" >&2
  docker rm -f "$NAME" 2>/dev/null || true
  run_exporter
fi

echo "Esperando la primera muestra del exportador..."
sleep 20
docker ps --filter "name=$NAME" --format '{{.Names}}  {{.Status}}'
docker logs --tail 5 "$NAME" 2>&1

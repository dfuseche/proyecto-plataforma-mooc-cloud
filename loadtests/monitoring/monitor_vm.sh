#!/usr/bin/env bash
# Captura metricas de infraestructura durante una corrida de k6, para
# cruzarlas con los resultados de loadtests/k6/. Correr EN CADA VM
# (Web Server y, cuando exista, Worker Server) via SSH, en paralelo a
# run_escenario1_niveles.ps1 / al script del Escenario 2.
#
# Uso:
#   ./monitor_vm.sh <nombre-contenedor-api> [intervalo_segundos] [archivo_salida.csv]
#
# Ejemplo (Web Server):
#   ./monitor_vm.sh mooc-api-server 5 web-server_$(date +%Y%m%d_%H%M%S).csv
#
# Detener con Ctrl+C cuando termine la corrida de k6 correspondiente.

set -euo pipefail

CONTAINER="${1:?Uso: ./monitor_vm.sh <contenedor> [intervalo] [salida.csv]}"
INTERVAL="${2:-5}"
OUT="${3:-metrics_$(hostname)_$(date +%Y%m%d_%H%M%S).csv}"

echo "host,timestamp,container_cpu_pct,container_mem_used_mb,container_mem_limit_mb,container_net_rx_mb,container_net_tx_mb,host_cpu_used_pct,host_mem_used_mb,host_mem_total_mb,host_load1,host_disk_used_pct,host_disk_read_kb_s,host_disk_write_kb_s,pg_active_connections" > "$OUT"

echo "Escribiendo metricas cada ${INTERVAL}s en $OUT (Ctrl+C para detener)..."

# Conexion a Postgres: usa el mismo DATABASE_URL del contenedor si esta
# disponible via docker inspect; si tu Postgres esta en otra VM/Cloud SQL,
# ajusta PG_CONN abajo o exportalo antes de correr este script.
PG_CONN="${PG_CONN:-$(docker inspect "$CONTAINER" --format='{{range .Config.Env}}{{println .}}{{end}}' 2>/dev/null | grep -m1 '^DATABASE_URL=' | cut -d= -f2-)}"

get_pg_active_connections() {
  if [ -z "${PG_CONN:-}" ]; then
    echo ""
    return
  fi
  docker run --rm postgres:16-alpine \
    psql "$PG_CONN" -t -A -c \
    "SELECT count(*) FROM pg_stat_activity WHERE state = 'active';" 2>/dev/null | tr -d '[:space:]' || echo ""
}

# Estado previo de /proc/diskstats para calcular KB/s leidos/escritos entre
# muestras (sector = 512 bytes). Vacio en la primera iteracion -> primera
# fila de disco I/O queda vacia, normal y esperado.
prev_disk_ts=""
prev_read_sectors=""
prev_write_sectors=""

while true; do
  ts=$(date -u +%Y-%m-%dT%H:%M:%SZ)
  now_epoch=$(date +%s)

  # --- Metricas del contenedor (docker stats, una sola muestra) ---
  stats=$(docker stats "$CONTAINER" --no-stream --format '{{.CPUPerc}}|{{.MemUsage}}|{{.NetIO}}' 2>/dev/null || echo "||")
  cpu_pct=$(echo "$stats" | cut -d'|' -f1 | tr -d '%')
  mem_raw=$(echo "$stats" | cut -d'|' -f2)
  mem_used=$(echo "$mem_raw" | awk -F' / ' '{print $1}')
  mem_limit=$(echo "$mem_raw" | awk -F' / ' '{print $2}')
  net_raw=$(echo "$stats" | cut -d'|' -f3)
  net_rx=$(echo "$net_raw" | awk -F' / ' '{print $1}')
  net_tx=$(echo "$net_raw" | awk -F' / ' '{print $2}')

  # --- Metricas del host (vmstat / free) ---
  host_cpu_idle=$(vmstat 1 2 | tail -1 | awk '{print $15}')
  host_cpu_used=$((100 - ${host_cpu_idle:-100}))
  mem_line=$(free -m | awk '/Mem:/ {print $3","$2}')
  host_mem_used=$(echo "$mem_line" | cut -d, -f1)
  host_mem_total=$(echo "$mem_line" | cut -d, -f2)
  load1=$(cut -d' ' -f1 /proc/loadavg)

  # --- Disco: % de uso del filesystem raiz y throughput agregado de
  # todos los discos fisicos (suma de /proc/diskstats, excluye loop/dm) ---
  disk_used_pct=$(df -P / | awk 'NR==2 {gsub("%","",$5); print $5}')

  read_sectors=0
  write_sectors=0
  while read -r _major _minor dev_name _reads_completed _reads_merged sectors_read _ms_reading _writes_completed _writes_merged sectors_written _rest; do
    case "$dev_name" in
      loop*|dm-*) continue ;;
    esac
    if [[ "$dev_name" =~ ^(sd|vd|nvme|xvd) ]]; then
      read_sectors=$((read_sectors + sectors_read))
      write_sectors=$((write_sectors + sectors_written))
    fi
  done < /proc/diskstats

  disk_read_kb_s=""
  disk_write_kb_s=""
  if [ -n "$prev_disk_ts" ]; then
    elapsed=$((now_epoch - prev_disk_ts))
    if [ "$elapsed" -gt 0 ]; then
      disk_read_kb_s=$(( (read_sectors - prev_read_sectors) * 512 / 1024 / elapsed ))
      disk_write_kb_s=$(( (write_sectors - prev_write_sectors) * 512 / 1024 / elapsed ))
    fi
  fi
  prev_disk_ts=$now_epoch
  prev_read_sectors=$read_sectors
  prev_write_sectors=$write_sectors

  pg_conns=$(get_pg_active_connections)

  echo "$(hostname),$ts,$cpu_pct,$mem_used,$mem_limit,$net_rx,$net_tx,$host_cpu_used,$host_mem_used,$host_mem_total,$load1,$disk_used_pct,$disk_read_kb_s,$disk_write_kb_s,$pg_conns" | tee -a "$OUT" >/dev/null

  sleep "$INTERVAL"
done

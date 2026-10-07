#!/usr/bin/env bash
# Respaldo de PostgreSQL para cumplir RPO <= 15 min (ver docs/OPERACIONES.md).
#
# Uso:
#   ./scripts/backup/backup_postgres.sh
#
# Pensado para correr contra el stack local de docker-compose.yml
# (servicio "postgres") via `docker compose exec`. Para programarlo
# cada 10-15 min, agregar a crontab (Linux/macOS/WSL) o a un Task
# Scheduler en Windows que invoque este script con Git Bash:
#
#   */10 * * * * cd /ruta/al/repo && ./scripts/backup/backup_postgres.sh >> scripts/backup/backup.log 2>&1
#
# Retiene los ultimos $RETENTION dumps y borra el resto para no llenar
# el disco.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
BACKUP_DIR="$SCRIPT_DIR/dumps"
RETENTION="${RETENTION:-20}"

DB_SERVICE="${DB_SERVICE:-postgres}"
DB_USER="${DB_USER:-mooc_user}"
DB_NAME="${DB_NAME:-mooc_db}"

mkdir -p "$BACKUP_DIR"

TIMESTAMP="$(date +%Y%m%d_%H%M%S)"
OUT_FILE="$BACKUP_DIR/${DB_NAME}_${TIMESTAMP}.sql.gz"

cd "$REPO_ROOT"

echo "[backup_postgres] $(date -Iseconds) Iniciando pg_dump de ${DB_NAME}..."

docker compose exec -T "$DB_SERVICE" pg_dump -U "$DB_USER" -d "$DB_NAME" | gzip > "$OUT_FILE"

SIZE="$(du -h "$OUT_FILE" | cut -f1)"
echo "[backup_postgres] OK -> $OUT_FILE ($SIZE)"

# Retencion: deja solo los $RETENTION dumps mas recientes.
cd "$BACKUP_DIR"
ls -1t "${DB_NAME}"_*.sql.gz 2>/dev/null | tail -n +$((RETENTION + 1)) | while read -r old; do
  echo "[backup_postgres] Eliminando dump antiguo: $old"
  rm -f "$old"
done

echo "[backup_postgres] Dumps actuales: $(ls -1 "${DB_NAME}"_*.sql.gz 2>/dev/null | wc -l)"

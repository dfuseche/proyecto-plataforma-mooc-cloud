#!/usr/bin/env bash
# Restauracion de PostgreSQL desde un dump generado por
# backup_postgres.sh, para medir y comprobar RTO (ver docs/OPERACIONES.md).
#
# Uso:
#   ./scripts/backup/restore_postgres.sh <archivo.sql.gz> [--force]
#
# DESTRUCTIVO: borra todo el esquema "public" actual de la base antes
# de restaurar. Pide confirmacion salvo que se pase --force (util para
# correrlo sin interaccion al cronometrar el RTO).

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

DB_SERVICE="${DB_SERVICE:-postgres}"
DB_USER="${DB_USER:-mooc_user}"
DB_NAME="${DB_NAME:-mooc_db}"

DUMP_FILE="${1:-}"
FORCE="${2:-}"

if [[ -z "$DUMP_FILE" ]]; then
  echo "Uso: $0 <archivo.sql.gz> [--force]" >&2
  echo "Dumps disponibles en $SCRIPT_DIR/dumps:" >&2
  ls -1t "$SCRIPT_DIR/dumps" 2>/dev/null >&2
  exit 1
fi

if [[ ! -f "$DUMP_FILE" ]]; then
  # Permite pasar solo el nombre de archivo si esta en scripts/backup/dumps/
  if [[ -f "$SCRIPT_DIR/dumps/$DUMP_FILE" ]]; then
    DUMP_FILE="$SCRIPT_DIR/dumps/$DUMP_FILE"
  else
    echo "No existe el archivo: $DUMP_FILE" >&2
    exit 1
  fi
fi

if [[ "$FORCE" != "--force" ]]; then
  read -r -p "Esto BORRA el esquema 'public' de '${DB_NAME}' y restaura desde '$DUMP_FILE'. Continuar? [y/N] " CONFIRM
  if [[ "$CONFIRM" != "y" && "$CONFIRM" != "Y" ]]; then
    echo "Cancelado."
    exit 1
  fi
fi

cd "$REPO_ROOT"

START_TS=$(date +%s)
echo "[restore_postgres] $(date -Iseconds) Iniciando restauracion desde $DUMP_FILE ..."

docker compose exec -T "$DB_SERVICE" psql -U "$DB_USER" -d "$DB_NAME" -v ON_ERROR_STOP=1 \
  -c "DROP SCHEMA public CASCADE; CREATE SCHEMA public;"

gunzip -c "$DUMP_FILE" | docker compose exec -T "$DB_SERVICE" psql -U "$DB_USER" -d "$DB_NAME" -v ON_ERROR_STOP=1

END_TS=$(date +%s)
ELAPSED=$((END_TS - START_TS))

echo "[restore_postgres] OK. Restauracion completada en ${ELAPSED}s."
echo "[restore_postgres] Verificando con un conteo rapido de filas por tabla:"
docker compose exec -T "$DB_SERVICE" psql -U "$DB_USER" -d "$DB_NAME" -c "
  SELECT schemaname, relname AS tabla, n_live_tup AS filas_aprox
  FROM pg_stat_user_tables
  ORDER BY relname;
"

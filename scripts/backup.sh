#!/usr/bin/env bash
# Postgres backup for terminal-card. Run from the repo root (next to compose.yaml).
#
# Cron (daily 03:30, keep 14 days):
#   30 3 * * * cd /srv/terminal-card && ./scripts/backup.sh >> backups/backup.log 2>&1
#
# Restore:
#   gunzip -c backups/terminal_card-YYYYMMDD-HHMMSS.sql.gz | \
#     docker compose exec -T db psql -U "$DB_USER" "$DB_NAME"
set -euo pipefail

[ -f .env ] && { set -a; . ./.env; set +a; }

DB_USER="${DB_USER:-postgres}"
DB_NAME="${DB_NAME:-terminal_card}"
BACKUP_DIR="${BACKUP_DIR:-backups}"
RETENTION_DAYS="${RETENTION_DAYS:-14}"

mkdir -p "$BACKUP_DIR"
out="$BACKUP_DIR/terminal_card-$(date +%Y%m%d-%H%M%S).sql.gz"

docker compose exec -T db pg_dump -U "$DB_USER" "$DB_NAME" | gzip >"$out"
echo "wrote $out"

find "$BACKUP_DIR" -name 'terminal_card-*.sql.gz' -mtime +"$RETENTION_DAYS" -delete

#!/usr/bin/env bash
# Nightly backup: database snapshot + images to S3-compatible storage via rclone.
# Cron (root): 30 3 * * * /srv/gallery/deploy/backup.sh >> /var/log/gallery-backup.log 2>&1
set -euo pipefail
cd "$(dirname "$0")"
set -a
. ./.env
set +a
: "${BACKUP_TARGET:?set BACKUP_TARGET in deploy/.env}"
stamp=$(date +%F)

docker compose exec -T app gallery backup /data/backup/gallery.db
rclone copyto ../data/backup/gallery.db "$BACKUP_TARGET/db/gallery-$stamp.db"
rclone sync ../data/images "$BACKUP_TARGET/images" --backup-dir "$BACKUP_TARGET/images-old/$stamp"
rclone delete --min-age 30d "$BACKUP_TARGET/db"
rclone delete --min-age 30d "$BACKUP_TARGET/images-old"
rclone rmdirs --leave-root "$BACKUP_TARGET/images-old"
echo "$(date -Is) backup $stamp done"

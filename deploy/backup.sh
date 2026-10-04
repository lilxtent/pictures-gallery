#!/usr/bin/env bash
# Nightly backup: database snapshot + images to S3-compatible storage via rclone.
# Cron (root): 30 3 * * * /srv/gallery/deploy/backup.sh >> /var/log/gallery-backup.log 2>&1
set -euo pipefail
cd "$(dirname "$0")"
# Read only BACKUP_TARGET; sourcing the whole file would break on passwords with spaces, $ or #.
BACKUP_TARGET=$(grep -E '^BACKUP_TARGET=' .env | tail -n 1 | cut -d= -f2- || true)
: "${BACKUP_TARGET:?set BACKUP_TARGET in deploy/.env}"
stamp=$(date +%F)

docker compose exec -T app gallery backup /data/backup/gallery.db
rclone copyto ../data/backup/gallery.db "$BACKUP_TARGET/db/gallery-$stamp.db"
rclone sync ../data/images "$BACKUP_TARGET/images" --backup-dir "$BACKUP_TARGET/images-old/$stamp"
rclone delete --min-age 30d "$BACKUP_TARGET/db"

# Prune images-old by the date in the directory name: files moved there keep their
# original modtime, so --min-age would delete them immediately. The directory may not
# exist yet (nothing replaced so far), which is fine.
cutoff=$(date -d '30 days ago' +%F 2>/dev/null || date -v-30d +%F)
{ rclone lsf --dirs-only "$BACKUP_TARGET/images-old" 2>/dev/null || true; } | while read -r dir; do
  dir=${dir%/}
  if [[ $dir =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}$ && $dir < $cutoff ]]; then
    rclone purge "$BACKUP_TARGET/images-old/$dir"
  fi
done
echo "$(date -Is) backup $stamp done"

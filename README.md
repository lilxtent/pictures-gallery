# Pictures gallery

Portfolio website for a watercolour painter: a public gallery in Russian and an
admin panel at `/admin` where she adds, crops, describes and orders her
paintings herself. Design: `docs/superpowers/specs/2026-10-04-painter-portfolio-design.md`.

## Local development

Requires Go 1.26+.

```bash
DEV=1 ADMIN_PASSWORD=dev-password go run ./cmd/gallery
```

- Site: http://localhost:8080 (an empty database is filled with three sample paintings)
- Admin: http://localhost:8080/admin, password `dev-password` (local development only)
- Data lives in `./data` (gitignored). Delete it to start over.
- Tests: `go test ./...`

Environment variables: `ADDR` (`:8080`), `DATA_DIR` (`./data`), `BASE_URL`
(`http://localhost:8080`), `ADMIN_PASSWORD` (first start only), `DEV=1`
(no Secure cookie, sample content), `TRUST_PROXY=1` (client IP from `X-Forwarded-For`).

## Production server (one-time setup)

1. Rent a small VPS with Ubuntu 24.04 at a Russian provider (e.g. Timeweb Cloud) and
   register a `.ru` domain. Point the `@` and `www` A records to the server's IP.
2. On the server:
   ```bash
   apt update && apt install -y docker.io docker-compose-v2 git rclone
   git clone https://github.com/lilxtent/pictures-gallery.git /srv/gallery
   cd /srv/gallery
   cp deploy/.env.example deploy/.env   # then edit DOMAIN, BASE_URL, ADMIN_PASSWORD, BACKUP_TARGET
   mkdir -p data && chown 10001:10001 data
   docker compose -f deploy/docker-compose.yml up -d --build
   ```
   If image pulls from Docker Hub fail, configure your provider's Docker Hub mirror
   in `/etc/docker/daemon.json` (`"registry-mirrors"`) and restart Docker.
3. Open `https://<domain>/admin`, log in with `ADMIN_PASSWORD`, and have your mother
   set her own password in «Настройки».

## Backups

1. Create an S3-compatible bucket at the same provider and run `rclone config`
   to add a remote named `backup` (type `s3`, provider `Other`, with the provider's
   endpoint and keys).
2. Set `BACKUP_TARGET=backup:<bucket>/gallery` in `deploy/.env`.
3. Test: `/srv/gallery/deploy/backup.sh`
4. Add to root's crontab (`crontab -e`):
   ```
   30 3 * * * /srv/gallery/deploy/backup.sh >> /var/log/gallery-backup.log 2>&1
   ```

Daily database snapshots are kept for 30 days; images are mirrored, and files
deleted or replaced on the site are kept in `images-old/<date>`, where each dated
directory is removed once its date is more than 30 days old.

**Restore** (on the server, in `/srv/gallery`, with `BACKUP_TARGET` as in `deploy/.env`):

```bash
docker compose -f deploy/docker-compose.yml stop app
rm -f data/gallery.db-wal data/gallery.db-shm      # stale journal files must not outlive the old db
rclone copyto "$BACKUP_TARGET/db/gallery-<date>.db" data/gallery.db
rclone sync "$BACKUP_TARGET/images" data/images
chown -R 10001:10001 data
docker compose -f deploy/docker-compose.yml start app
```

## Updating

```bash
DEPLOY_HOST=root@<server-ip> deploy/deploy.sh
```

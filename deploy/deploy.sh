#!/usr/bin/env bash
# Update the server to the latest main:
#   DEPLOY_HOST=root@203.0.113.10 deploy/deploy.sh
set -euo pipefail
: "${DEPLOY_HOST:?set DEPLOY_HOST, e.g. root@203.0.113.10}"
DEPLOY_DIR=${DEPLOY_DIR:-/srv/gallery}
ssh "$DEPLOY_HOST" "cd '$DEPLOY_DIR' && git pull --ff-only && docker compose -f deploy/docker-compose.yml up -d --build && docker image prune -f"

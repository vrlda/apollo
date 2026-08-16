#!/bin/bash
set -e

echo "=== AgentHQ Deploy ==="

echo "→ Building frontend..."
cd "$(dirname "$0")/frontend"
npm run build

echo "→ Syncing frontend..."
rsync -az --delete frontend/dist/ prod:/home/deploy/agenthq/frontend/dist/

echo "→ Syncing backend source..."
rsync -az --delete \
  --exclude='*.db' \
  --exclude='agenthq-server' \
  --exclude='.env' \
  --exclude='workspace/' \
  backend/ \
  prod:/home/deploy/agenthq/backend/

echo "→ Building binary on server..."
ssh prod "cd /home/deploy/agenthq/backend && CGO_ENABLED=1 /usr/local/go/bin/go build -o agenthq-server ."

echo "→ Restarting service..."
ssh prod "sudo systemctl restart agenthq && sleep 2 && sudo systemctl status agenthq --no-pager | head -8"

echo "=== Done: https://agenthq.one ==="

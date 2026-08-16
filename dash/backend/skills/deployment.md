---
description: Docker and deployment — Dockerfile best practices, docker-compose, systemd services, nginx reverse proxy, zero-downtime deploys
---

# Deployment Skill

## Dockerfile Best Practices

### Multi-stage Go build
```dockerfile
FROM golang:1.24-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download          # cache dependency layer separately
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o server .

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=builder /app/server .
COPY --from=builder /app/skills ./skills
EXPOSE 4000
CMD ["./server"]
```

### Layer caching rules
- Copy `go.mod` + `go.sum` BEFORE source code — `go mod download` cache only invalidates when dependencies change
- Put rarely-changing layers (apt installs) before frequently-changing ones (source code)
- Use `.dockerignore` to exclude `node_modules`, `.git`, `dist`

## Docker Compose
```yaml
services:
  agenthq:
    build: ./dash/backend
    ports: ["4000:4000"]
    environment:
      - OPENROUTER_API_KEY=${OPENROUTER_API_KEY}
      - AGENTHQ_WORKSPACE_ROOT=/app/data/workspaces
    volumes:
      - agenthq-data:/app/data
    restart: unless-stopped
  
  frontend:
    build: ./dash/frontend
    depends_on: [agenthq]
    
volumes:
  agenthq-data:
```

## systemd Service
```ini
# /etc/systemd/system/apollo.service
[Unit]
Description=Apollo Dashboard
After=network.target

[Service]
Type=simple
User=apollo
WorkingDirectory=/opt/apollo
ExecStart=/opt/apollo/server
Restart=on-failure
RestartSec=5s
Environment=PORT=4000
EnvironmentFile=/opt/apollo/.env

[Install]
WantedBy=multi-user.target
```
```bash
systemctl daemon-reload && systemctl enable --now apollo
journalctl -u apollo -f  # follow logs
```

## nginx Reverse Proxy
```nginx
server {
    listen 443 ssl;
    server_name apollo.yourdomain.com;

    ssl_certificate /etc/letsencrypt/live/apollo.yourdomain.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/apollo.yourdomain.com/privkey.pem;

    location / {
        proxy_pass http://localhost:4000;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";  # WebSocket support
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        # SSE: disable buffering
        proxy_buffering off;
        proxy_cache off;
        proxy_read_timeout 300s;
    }
}
```

## Zero-Downtime Deploy
```bash
# Build new binary
go build -o server.new .

# Swap atomically
mv server server.old && mv server.new server

# Graceful restart (sends SIGTERM → process drains → exits)
kill -SIGTERM $(pidof server)
# Or with systemd:
systemctl reload apollo   # if ExecReload is configured
systemctl restart apollo  # hard restart

# Keep old binary for quick rollback
```

## SSL Certificate (Let's Encrypt)
```bash
certbot --nginx -d apollo.yourdomain.com
# Auto-renewal (already installed by certbot):
systemctl status certbot.timer
```

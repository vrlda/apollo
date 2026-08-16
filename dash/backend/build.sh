#!/bin/bash
# Unified build script for Apollo Dashboard

# Ensure we are in the script's directory (dash/backend)
cd "$(dirname "$0")"

echo "----------------------------------------"
echo "🚀 Starting Full Production Build..."
echo "----------------------------------------"

# 1. Build Backend for Linux (AMD64 + ARM64) using Zig cross-compiler
# This avoids macOS Mach-O binaries being deployed to Linux servers.
if ! command -v zig >/dev/null 2>&1; then
    echo "❌ Error: 'zig' is required for Linux cross-builds with cgo."
    echo "Install it once via: brew install zig"
    exit 1
fi

echo "🔨 Building Backend (Linux AMD64)..."
CC='zig cc -target x86_64-linux-gnu' \
CXX='zig c++ -target x86_64-linux-gnu' \
CGO_ENABLED=1 GOOS=linux GOARCH=amd64 \
go build -o apollo-dash-linux-amd64 .
if [ $? -ne 0 ]; then
    echo "❌ Error: Backend AMD64 build failed."
    exit 1
fi
echo "✅ Success: apollo-dash-linux-amd64 created."

echo "🔨 Building Backend (Linux ARM64)..."
CC='zig cc -target aarch64-linux-gnu' \
CXX='zig c++ -target aarch64-linux-gnu' \
CGO_ENABLED=1 GOOS=linux GOARCH=arm64 \
go build -o apollo-dash-linux-arm64 .
if [ $? -ne 0 ]; then
    echo "❌ Error: Backend ARM64 build failed."
    exit 1
fi
echo "✅ Success: apollo-dash-linux-arm64 created."

# 1b. Create runtime launcher expected by systemd at /opt/apollo-dash/backend/apollo-dash
cat > apollo-dash <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
ARCH="$(uname -m)"
BASE_DIR="$(cd "$(dirname "$0")" && pwd)"
case "$ARCH" in
  x86_64|amd64)
    exec "$BASE_DIR/apollo-dash-linux-amd64" "$@"
    ;;
  aarch64|arm64)
    exec "$BASE_DIR/apollo-dash-linux-arm64" "$@"
    ;;
  *)
    echo "Unsupported architecture: $ARCH" >&2
    exit 1
    ;;
esac
EOF
chmod +x apollo-dash
echo "✅ Success: launcher 'apollo-dash' created."

# 2. Build Frontend Production Bundle
echo "🌐 Building Frontend..."
cd ../frontend
# We run npm run build to generate the dist/ folder
# Use npm install only if needed, but usually assume environment is ready
npm run build
if [ $? -eq 0 ]; then
    echo "✅ Success: Frontend built."
else
    echo "❌ Error: Frontend build failed."
    exit 1
fi

echo "----------------------------------------"
echo "🎉 Build Complete!"
echo "Next step: Sync to your Ubuntu server using rsync (CAUTION: EXCLUDING .db TO PREVENT DATA LOSS):"
echo "rsync -avz --exclude 'frontend/node_modules' --exclude '*.db' dash/ dan@apollo:/opt/apollo-dash/"
echo "----------------------------------------"

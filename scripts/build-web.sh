#!/usr/bin/env sh
# Build the frontend bundle into apps/web/dist.
set -eu

WEB_DIR="$(CDPATH='' cd -- "$(dirname -- "$0")/../apps/web" && pwd)"

# Shared with check-web.sh: reinstalls when package-lock.json is newer than the
# installed tree, so a bundle is never built from stale dependencies.
"$(dirname -- "$0")/web-deps.sh"

cd "$WEB_DIR"

echo "==> vite build"
npm run --silent build

echo "==> $WEB_DIR/dist"

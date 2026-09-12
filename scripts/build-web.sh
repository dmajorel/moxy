#!/usr/bin/env sh
# Build the frontend bundle into apps/web/dist.
set -eu

WEB_DIR="$(CDPATH='' cd -- "$(dirname -- "$0")/../apps/web" && pwd)"
cd "$WEB_DIR"

if [ ! -d node_modules ]; then
	if [ -f package-lock.json ]; then
		echo "==> npm ci"
		npm ci --no-audit --no-fund
	else
		echo "==> npm install"
		npm install --no-audit --no-fund
	fi
fi

echo "==> vite build"
npm run --silent build

echo "==> $WEB_DIR/dist"

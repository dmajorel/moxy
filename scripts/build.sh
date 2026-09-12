#!/usr/bin/env sh
# Build the moxyd binary into bin/.
set -eu
. "$(dirname -- "$0")/env.sh"

VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"
OUT="$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)/bin/moxyd"

mkdir -p "$(dirname -- "$OUT")"
cd "$API_DIR"
go build -trimpath \
	-ldflags "-s -w -X github.com/dmajorel/moxy/apps/api/internal/server.Version=$VERSION" \
	-o "$OUT" ./cmd/moxyd

echo "==> $OUT ($VERSION)"

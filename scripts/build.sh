#!/usr/bin/env sh
# Build the moxyd binary into bin/.
set -eu
# shellcheck source=scripts/env.sh
. "$(dirname -- "$0")/env.sh"

VERSION="$("$(dirname -- "$0")/version.sh")"
OUT="$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)/bin/moxyd"

mkdir -p "$(dirname -- "$OUT")"
cd "$API_DIR"
# -buildvcs=false, not the "auto" default: auto stamps vcs.revision and vcs.time
# when .git is reachable and nothing when it is not, so the same commit built
# locally and built in the image stage (where .dockerignore drops .git) yields
# two different binaries. The version above already carries the git describe,
# and a reproducible byte-for-byte build is worth more than a second copy of it.
go build -trimpath -buildvcs=false \
	-ldflags "-s -w -X github.com/dmajorel/moxy/apps/api/internal/server.Version=$VERSION" \
	-o "$OUT" ./cmd/moxyd

echo "==> $OUT ($VERSION)"

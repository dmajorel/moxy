#!/usr/bin/env sh
# Static checks and tests for the backend, and for the frontend when one is
# present (see the guard below).
set -eu
. "$(dirname -- "$0")/env.sh"

# Resolved before any cd: the backend steps below change directory.
SCRIPTS_DIR="$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)"

echo "==> gofmt"
unformatted="$(gofmt -l "$API_DIR")"
if [ -n "$unformatted" ]; then
	echo "unformatted files (run './scripts/fmt.sh', or 'make fmt'):"
	echo "$unformatted"
	exit 1
fi

echo "==> go vet"
cd "$API_DIR" && go vet ./...

echo "==> go test"
go test ./...

# The frontend is optional. Guarding on apps/web/package.json keeps this script
# the single verification contract of the repository while letting a checkout
# without a frontend (or one made before the scaffolding landed) still pass,
# and keeps the backend from requiring a Node toolchain to be verified.
# MOXY_CHECK_WEB=0 opts out explicitly, which is what CI does in the backend
# job: the frontend has a job of its own there and must not run twice.
if [ "${MOXY_CHECK_WEB:-1}" = "1" ] && [ -f "$SCRIPTS_DIR/../apps/web/package.json" ]; then
	"$SCRIPTS_DIR/check-web.sh"
else
	echo "==> skipping web checks"
fi

echo "==> OK"

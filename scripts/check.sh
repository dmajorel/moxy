#!/usr/bin/env sh
# Static checks and tests for the backend, and for the frontend when one is
# present (see the guard below).
set -eu
# shellcheck source=scripts/env.sh
. "$(dirname -- "$0")/env.sh"

# Resolved before any cd: the backend steps below change directory.
SCRIPTS_DIR="$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)"

echo "==> gofmt"
unformatted="$(gofmt -l "$API_DIR")"
if [ -n "$unformatted" ]; then
	echo "unformatted files (run 'gofmt -w apps/api'):"
	echo "$unformatted"
	exit 1
fi

echo "==> go vet"
cd "$API_DIR" && go vet ./...

echo "==> go test"
# MOXY_COVER names a coverage profile to write, resolved against apps/api.
# Unset by default, and CI is the only caller that sets it: a profile written
# on every local run would leave a file behind for the "working tree is clean"
# step to trip over, and reading it is not part of what `make check` answers.
# -covermode=atomic because that is the mode a profile merged across packages
# has to be in; -race, which would normally come with it, is not available here
# at all — the race detector needs CGO, and CGO_ENABLED=0 is the whole point.
if [ -n "${MOXY_COVER:-}" ]; then
	go test -coverprofile="$MOXY_COVER" -covermode=atomic ./...
else
	go test ./...
fi

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

#!/usr/bin/env sh
# Static checks and tests for the backend.
set -eu
. "$(dirname -- "$0")/env.sh"

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
go test ./...

echo "==> OK"

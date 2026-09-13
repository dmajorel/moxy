#!/usr/bin/env sh
# Format the backend in place.
#
# check.sh tells the reader to "run gofmt -w apps/api" when it finds an
# unformatted file; this is that command, so the advice points at something the
# repository actually offers instead of at a path to retype. Writing, unlike
# every other script here, hence its own name rather than a flag on check.sh.
set -eu
. "$(dirname -- "$0")/env.sh"

echo "==> gofmt -w"
changed="$(gofmt -l -w "$API_DIR")"
if [ -n "$changed" ]; then
	echo "$changed"
else
	echo "nothing to format"
fi

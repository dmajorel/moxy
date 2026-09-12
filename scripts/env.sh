#!/usr/bin/env sh
# Shared build environment.
#
# The backend is built on the Go standard library alone (see CLAUDE.md): no module
# proxy is needed, and disabling it makes the build fail loudly and immediately if
# an external dependency ever creeps into the code.
set -eu

export GOPROXY=off
export GOFLAGS=-mod=mod
export CGO_ENABLED=0

API_DIR="$(CDPATH='' cd -- "$(dirname -- "$0")/../apps/api" && pwd)"
export API_DIR

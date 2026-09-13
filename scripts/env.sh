#!/usr/bin/env sh
# Shared build environment.
#
# The backend is built on the Go standard library alone (see CLAUDE.md): no module
# proxy is needed, and disabling it makes the build fail loudly and immediately if
# an external dependency ever creeps into the code.
set -eu

export GOPROXY=off
# GOFLAGS is deliberately left alone: -mod=mod used to be set here, and it let
# the toolchain rewrite go.mod and go.sum on the fly. The default since Go 1.16
# is -mod=readonly, which fails instead — an intruding dependency then stops the
# build with a plain error rather than being quietly written into go.mod.
export CGO_ENABLED=0
# The installed toolchain is the one that builds, always. Go 1.21+ would
# otherwise download the version named by go.mod, which GOPROXY=off turns into
# an obscure failure; "local" makes the mismatch a plain, readable error
# instead. Go 1.19 does not know this variable and ignores it.
export GOTOOLCHAIN=local

API_DIR="$(CDPATH='' cd -- "$(dirname -- "$0")/../apps/api" && pwd)"
export API_DIR

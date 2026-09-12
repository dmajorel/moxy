#!/usr/bin/env sh
# Build the moxy container image from Containerfile.
#
# Uses podman when available, docker otherwise; MOXY_CONTAINER_ENGINE forces
# one. IMAGE and TAG name the result, VERSION is what /healthz will report.
set -eu

ROOT="$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)"
IMAGE="${IMAGE:-ghcr.io/dmajorel/moxy}"
TAG="${TAG:-dev}"
VERSION="${VERSION:-$(git -C "$ROOT" describe --tags --always --dirty 2>/dev/null || echo dev)}"

ENGINE="${MOXY_CONTAINER_ENGINE:-}"
if [ -z "$ENGINE" ]; then
	if command -v podman >/dev/null 2>&1; then
		ENGINE=podman
	elif command -v docker >/dev/null 2>&1; then
		ENGINE=docker
	else
		echo "no container engine found: install podman or docker, or set MOXY_CONTAINER_ENGINE" >&2
		exit 1
	fi
fi

echo "==> $ENGINE build"
"$ENGINE" build \
	-f "$ROOT/Containerfile" \
	--build-arg "VERSION=$VERSION" \
	-t "$IMAGE:$TAG" \
	"$ROOT"

echo "==> $IMAGE:$TAG ($VERSION)"

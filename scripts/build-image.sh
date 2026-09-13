#!/usr/bin/env sh
# Build the moxy container image from Containerfile.
#
# Uses podman when available, docker otherwise; MOXY_CONTAINER_ENGINE forces
# one. IMAGE and TAG name the result, VERSION is what /healthz will report.
#
# TARGET picks the final stage: "moxy" is the shipped image, with no shell and
# no HTTP client; "debug" is the opt-in variant that adds bash and curl for
# diagnosis. The variant must never reach the plain tag someone deploys, so the
# suffix is applied here rather than left to the caller, and an unknown target
# is refused instead of quietly producing an unsuffixed tag.
set -eu

ROOT="$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)"
IMAGE="${IMAGE:-ghcr.io/dmajorel/moxy}"
TAG="${TAG:-dev}"
TARGET="${TARGET:-moxy}"
case "$TARGET" in
moxy) ;;
debug) TAG="$TAG-debug" ;;
*)
	echo "unknown TARGET '$TARGET': expected moxy or debug" >&2
	exit 2
	;;
esac
VERSION="${VERSION:-$(git -C "$ROOT" describe --tags --always --dirty 2>/dev/null || echo dev)}"
# The CI image gets these from docker/metadata-action; without them here, a
# locally built image cannot be traced back to a commit. Unknown outside a
# checkout, which is the one case where there is nothing to point at.
REVISION="$(git -C "$ROOT" rev-parse HEAD 2>/dev/null || echo unknown)"
CREATED="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

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

echo "==> $ENGINE build ($TARGET)"
"$ENGINE" build \
	-f "$ROOT/Containerfile" \
	--target "$TARGET" \
	--build-arg "VERSION=$VERSION" \
	--label "org.opencontainers.image.revision=$REVISION" \
	--label "org.opencontainers.image.created=$CREATED" \
	-t "$IMAGE:$TAG" \
	"$ROOT"

echo "==> $IMAGE:$TAG ($VERSION, target $TARGET)"

#!/usr/bin/env sh
# Print the version stamped into the binary and the image.
#
# Single implementation on purpose: build.sh bakes it into moxyd (/healthz
# reports it), build-image.sh passes it as a build argument, and the Image
# workflow feeds it to the same Containerfile. Three copies of one git describe
# drift, and the first copy to drift is the one nobody reads.
#
# --match 'v*' ties the version to release tags: without it, the first
# unrelated tag pushed to the repository would silently become the version.
# --dirty marks a build made from a modified tree, which is what a local build
# usually is. VERSION from the environment wins, which is how the container
# stages get a version at all: .dockerignore keeps .git out of their context.
set -eu

ROOT="$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)"

echo "${VERSION:-$(git -C "$ROOT" describe --tags --match 'v*' --always --dirty 2>/dev/null || echo dev)}"

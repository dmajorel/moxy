# moxy container image: the moxyd daemon serving both the API and the built
# frontend bundle from a single, shell-less, non-root image.
#
# Build with ./scripts/build-image.sh (or podman/docker build -f Containerfile).
# Two final stages: "moxy" (default, shipped, no shell) and "debug", an opt-in
# variant carrying bash and curl for diagnosis — never the deployed image.
# The build stages run on the build platform and cross-compile, so a
# multi-architecture image needs no emulation. The debug stage is the one
# exception: its apt layer runs on the target platform.

# --- Frontend bundle -------------------------------------------------------
# The bundle is architecture independent: always build it natively.
FROM --platform=$BUILDPLATFORM docker.io/library/node:26-bookworm-slim@sha256:cd9f682fa2885cd1056e830424764158570061c59736a1da836bc3d73df095ae AS web
WORKDIR /src
# Dependencies first, so the (slow) npm ci layer survives source changes.
COPY apps/web/package.json apps/web/package-lock.json apps/web/
RUN cd apps/web && npm ci --no-audit --no-fund
# Only the one script this stage runs: copying the whole directory made every
# edit to probe-pve.sh or prune-images.sh invalidate the bundle build.
COPY scripts/build-web.sh scripts/
COPY apps/web/ apps/web/
# build-web.sh reinstalls only when package-lock.json is newer than the tree
# installed above, which the layer order makes false: the install ran last.
RUN ./scripts/build-web.sh

# --- Backend binary --------------------------------------------------------
# The toolchain here is deliberately NOT the "go 1.19" of apps/api/go.mod. That
# directive sets the LANGUAGE level; the standard library linked into the binary
# comes from whatever toolchain compiles it. Building the release with 1.19 —
# whose last patch was 1.19.13, September 2023 — shipped a standard library with
# dozens of unfixed advisories in crypto/tls, crypto/x509 and net/http, in a
# daemon that terminates HTTP and parses certificates its peers control. So the
# image builds with a supported Go series, and go.mod keeps 1.19 as the language
# level, which is what stops newer standard library APIs from creeping in.
FROM --platform=$BUILDPLATFORM docker.io/library/golang:1.27-bookworm@sha256:648f440f42a0958804efb24df176f806f9d353b41f1c0627f666428e40310f6b AS api
WORKDIR /src
ARG VERSION=dev
ARG TARGETOS
ARG TARGETARCH
# build.sh and the env.sh it sources, and nothing else: same reason as above.
COPY scripts/env.sh scripts/build.sh scripts/
COPY apps/api/ apps/api/
# env.sh sets CGO_ENABLED=0 and GOPROXY=off: the binary is static and the
# build needs no network, since the backend has no dependency to download.
RUN GOOS=$TARGETOS GOARCH=$TARGETARCH VERSION=$VERSION ./scripts/build.sh

# --- Debug variant ---------------------------------------------------------
# Opt-in only: `--target debug`, or TARGET=debug ./scripts/build-image.sh. The
# distroless stage below is the LAST one in the file, so a build that does not
# ask for this stage by name cannot end up with it.
#
# Why it is a separate image rather than two more packages in the final one:
# curl inside a process that holds hypervisor tokens is a ready-made
# exfiltration primitive, and a shell turns a file read into something
# exploitable. This variant is meant to be run in place of the production image
# for as long as a diagnosis takes, and never to be deployed.
#
# Everything below the apt layer MIRRORS the final stage — same artifacts, same
# paths, same environment, same uid, same entrypoint, same probe. A debug image
# that does not reproduce the runtime it is meant to explain proves nothing, so
# the two blocks are edited together or not at all.
FROM docker.io/library/debian:12-slim@sha256:88200866dfff7ea7f5cbcb6ec7c8a701889efe6fe859fe64d6990e4b07ea4171 AS debug
ARG VERSION=dev

# bash and curl are the point of the variant; ca-certificates restores what
# distroless ships by default and what tls.mode "system" needs. The package
# lists are dropped so the layer does not carry an index that is stale the day
# after. Unlike every other stage here, this RUN executes on the TARGET
# platform: building the variant for a foreign architecture needs emulation,
# which is why CI publishes it for linux/amd64 only.
RUN apt-get update \
    && apt-get install -y --no-install-recommends bash ca-certificates curl \
    && rm -rf /var/lib/apt/lists/*
# distroless ships uid/gid 65532 as "nonroot"; debian does not, so recreate it
# with the same numeric ids. Those numbers are what the permissions of a
# mounted /etc/moxy are checked against, and reading them as the wrong uid is
# precisely the diagnosis this image exists for.
RUN groupadd --gid 65532 nonroot \
    && useradd --uid 65532 --gid 65532 --home-dir /home/nonroot --create-home \
        --shell /bin/bash nonroot

COPY --from=api /src/bin/moxyd /usr/local/bin/moxyd
COPY --from=web /src/apps/web/dist /srv/moxy/web

ENV MOXY_ADDR=0.0.0.0:8080 \
    MOXY_WEB=/srv/moxy/web \
    MOXY_CONFIG=/etc/moxy/config.json

EXPOSE 8080
USER nonroot:nonroot

LABEL org.opencontainers.image.source="https://github.com/dmajorel/moxy" \
      org.opencontainers.image.description="Multi-cluster web overlay for Proxmox VE — debug variant: adds bash and curl, not for production" \
      org.opencontainers.image.version="$VERSION"

HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD ["/usr/local/bin/moxyd", "-healthcheck"]

ENTRYPOINT ["/usr/local/bin/moxyd"]

# --- Final image -----------------------------------------------------------
# distroless/static ships CA certificates (needed by tls.mode "system") and a
# nonroot user, and nothing else: no shell, no package manager. This is the only
# layer present at runtime, so it is the one a moved tag would silently swap
# under every image published afterwards: pinned by digest like the two build
# stages above. The digest is the multi-arch index, so it still resolves for
# both linux/amd64 and linux/arm64. Dependabot's docker ecosystem moves it.
#
# Last stage of the file, hence what a build without --target produces: the
# shipped image is the one you get by default, and the debug variant above has
# to be asked for by name. The block below is mirrored there; keep the two in
# step.
FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab AS moxy
ARG VERSION=dev

COPY --from=api /src/bin/moxyd /usr/local/bin/moxyd
COPY --from=web /src/apps/web/dist /srv/moxy/web

# 0.0.0.0 is required for the published port to reach the process; the README
# explains why that port must stay on loopback or a private network.
ENV MOXY_ADDR=0.0.0.0:8080 \
    MOXY_WEB=/srv/moxy/web \
    MOXY_CONFIG=/etc/moxy/config.json

EXPOSE 8080
USER nonroot:nonroot

LABEL org.opencontainers.image.source="https://github.com/dmajorel/moxy" \
      org.opencontainers.image.description="Multi-cluster web overlay for Proxmox VE" \
      org.opencontainers.image.version="$VERSION"

# The image has no shell and no curl, so the only executable a HEALTHCHECK can
# run is moxyd itself: -healthcheck does one GET to /healthz on the local
# address and exits 0 or 1. It reports LIVENESS — the process answers — and
# deliberately not readiness: a cluster that takes its time to answer is not a
# reason to restart the daemon. Gate traffic on /readyz from the orchestrator.
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD ["/usr/local/bin/moxyd", "-healthcheck"]

ENTRYPOINT ["/usr/local/bin/moxyd"]

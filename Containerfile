# moxy container image: the moxyd daemon serving both the API and the built
# frontend bundle from a single, shell-less, non-root image.
#
# Build with ./scripts/build-image.sh (or podman/docker build -f Containerfile).
# The build stages run on the build platform and cross-compile, so a
# multi-architecture image needs no emulation.

# --- Frontend bundle -------------------------------------------------------
# The bundle is architecture independent: always build it natively.
FROM --platform=$BUILDPLATFORM docker.io/library/node:22-bookworm-slim@sha256:83f487e0a63425e5b4d146fb5e5be574bcbe1b7b843d3ebafdd95eaf7767a7e5 AS web
WORKDIR /src
# Dependencies first, so the (slow) npm ci layer survives source changes.
COPY apps/web/package.json apps/web/package-lock.json apps/web/
RUN cd apps/web && npm ci --no-audit --no-fund
COPY scripts/ scripts/
COPY apps/web/ apps/web/
# build-web.sh skips the install when node_modules already exists.
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
COPY scripts/ scripts/
COPY apps/api/ apps/api/
# env.sh sets CGO_ENABLED=0 and GOPROXY=off: the binary is static and the
# build needs no network, since the backend has no dependency to download.
RUN GOOS=$TARGETOS GOARCH=$TARGETARCH VERSION=$VERSION ./scripts/build.sh

# --- Final image -----------------------------------------------------------
# distroless/static ships CA certificates (needed by tls.mode "system") and a
# nonroot user, and nothing else: no shell, no package manager.
# Pinned by tag only: gcr.io was not reachable from the environment where this
# file was written, so the digest could not be recorded. Pin it when you can.
FROM gcr.io/distroless/static-debian12:nonroot
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

# No HEALTHCHECK: the image has no shell or HTTP client to run one. Probe
# GET /healthz from the orchestrator instead.
ENTRYPOINT ["/usr/local/bin/moxyd"]

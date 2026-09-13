# moxy — developer entry points.
#
# These targets are thin wrappers around scripts/: the scripts stay the single
# source of truth because CI runs them directly, and duplicating their logic
# here would let the two drift apart. Make is a convenience, not a second build
# system.

SHELL := /bin/sh

.DEFAULT_GOAL := help

.PHONY: help all check check-api check-web analyze fmt build build-web image mock serve dev probe clean

help: ## list the available targets
	@printf 'moxy — usage: make <target>\n\n'
	@awk 'BEGIN {FS = ":.*## "} /^[a-z][a-z-]*:.*## / {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)
	@printf '\n  probe needs MOXY_SECRET in the environment, plus URL= and TOKEN=;\n'
	@printf '  add INSECURE=1 to skip TLS verification.\n\n'

all: check build build-web ## verify everything, then build both sides

check: ## run every check, backend and frontend
	@./scripts/check.sh

check-api: ## backend only: gofmt, go vet, go test
	@MOXY_CHECK_WEB=0 ./scripts/check.sh

check-web: ## frontend only: typecheck, eslint, vitest
	@./scripts/check-web.sh

analyze: ## shellcheck, staticcheck, govulncheck, npm audit (tools installed separately)
	@./scripts/analyze.sh

fmt: ## format the backend in place (gofmt -w)
	@./scripts/fmt.sh

build: ## compile bin/moxyd
	@./scripts/build.sh

build-web: ## bundle the frontend into apps/web/dist
	@./scripts/build-web.sh

image: ## build the OCI image (podman or docker)
	@./scripts/build-image.sh

mock: build ## run moxyd with the sample data, no cluster contacted
	@./bin/moxyd -mock

serve: build build-web ## run moxyd serving the bundle, one origin, sample data
	@./bin/moxyd -mock -web apps/web/dist

dev: ## run the mock daemon and the Vite dev server together
	@./scripts/dev.sh

probe: ## probe a live PVE cluster read-only: make probe URL=… TOKEN=…
	@./scripts/probe-pve.sh $(URL) $(TOKEN) $(if $(INSECURE),--insecure)

clean: ## remove build artifacts
	@rm -rf bin apps/web/dist
	@echo "==> removed bin/ and apps/web/dist/"

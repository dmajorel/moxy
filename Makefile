# moxy — developer entry points.
#
# These targets are thin wrappers around scripts/: the scripts stay the single
# source of truth because CI runs them directly, and duplicating their logic
# here would let the two drift apart. Make is a convenience, not a second build
# system.

SHELL := /bin/sh

.DEFAULT_GOAL := help

.PHONY: help all check check-api check-web build build-web image mock release clean

help: ## list the available targets
	@printf 'moxy — usage: make <target>\n\n'
	@awk 'BEGIN {FS = ":.*## "} /^[a-z][a-z-]*:.*## / {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)
	@printf '\n'

all: check build build-web ## verify everything, then build both sides

check: ## run every check, backend and frontend
	@./scripts/check.sh

check-api: ## backend only: gofmt, go vet, go test
	@MOXY_CHECK_WEB=0 ./scripts/check.sh

check-web: ## frontend only: typecheck, eslint, vitest
	@./scripts/check-web.sh

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

release: ## rehearse a release: make release VERSION=v0.1.0 (see docs/RELEASE.md)
	@./scripts/release.sh "$(VERSION)"

clean: ## remove build artifacts
	@rm -rf bin apps/web/dist
	@echo "==> removed bin/ and apps/web/dist/"

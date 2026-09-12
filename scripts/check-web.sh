#!/usr/bin/env sh
# Static checks and tests for the frontend.
#
# The backend equivalent is check.sh; this script is its counterpart for
# apps/web and follows the same contract: readable "==>" headers, and a
# non-zero exit as soon as a step fails.
set -eu

# Resolved from the script's own location, like env.sh and build.sh, so the
# script works from any current directory.
WEB_DIR="$(CDPATH='' cd -- "$(dirname -- "$0")/../apps/web" && pwd)"
cd "$WEB_DIR"

if [ ! -d node_modules ]; then
	# npm ci is the reproducible install: it obeys the lockfile exactly and
	# refuses to run without one. npm install is the fallback for a tree that
	# has no lockfile yet.
	if [ -f package-lock.json ]; then
		echo "==> npm ci"
		npm ci --no-audit --no-fund
	else
		echo "==> npm install"
		npm install --no-audit --no-fund
	fi
else
	echo "==> dependencies already installed (skipping npm ci)"
fi

echo "==> tsc"
npm run --silent typecheck

echo "==> eslint"
npm run --silent lint

echo "==> vitest"
npm run --silent test

echo "==> web OK"

#!/usr/bin/env sh
# Install the frontend dependencies when they are missing or out of date.
#
# Shared by check-web.sh and build-web.sh so both answer the question the same
# way. The guard used to be "node_modules exists", which made a git pull that
# moves package-lock.json lint, typecheck and test against the previous
# dependency tree without a word. npm writes node_modules/.package-lock.json at
# the end of an install, so comparing the two timestamps says whether the
# installed tree still matches the lockfile that produced it. CI installs
# before calling the check scripts and is therefore still a single install.
#
# -nt is not in POSIX.1-2017 but is implemented by dash, bash, ksh, zsh and
# busybox ash, which covers every shell this repository runs under. A shell
# without it errors out rather than skipping silently, which is the failure
# mode to prefer here.
set -eu

WEB_DIR="$(CDPATH='' cd -- "$(dirname -- "$0")/../apps/web" && pwd)"
cd "$WEB_DIR"

# npm ci is the reproducible install: it obeys the lockfile exactly and refuses
# to run without one. npm install is the fallback for a tree that has no
# lockfile yet, and there is then nothing to compare a stamp against.
if [ ! -f package-lock.json ]; then
	if [ -d node_modules ]; then
		echo "==> dependencies already installed (no lockfile)"
	else
		echo "==> npm install"
		npm install --no-audit --no-fund
	fi
	exit 0
fi

stamp=node_modules/.package-lock.json
# find -newer rather than [ -nt ]: the test is a ksh extension that POSIX sh
# does not define, and this script runs under whatever /usr/bin/env sh is.
if [ -d node_modules ] && [ -f "$stamp" ] &&
	[ -z "$(find package-lock.json -newer "$stamp" 2>/dev/null)" ]; then
	echo "==> dependencies up to date (skipping npm ci)"
else
	echo "==> npm ci"
	npm ci --no-audit --no-fund
fi

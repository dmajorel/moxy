#!/usr/bin/env sh
# Static analysis and vulnerability scanning, the checks that need a tool
# nobody has to have installed to build moxy.
#
# Separate from check.sh on purpose. check.sh has to stay runnable with the
# local Go toolchain and nothing else, and every analyzer here breaks that:
# staticcheck and govulncheck both need a Go newer than the 1.19 of go.mod, and
# both are downloaded from the module proxy that env.sh deliberately turns off.
# Installing them is the caller's job — the CI job installs them outside the
# module, so they never enter go.mod nor bin/moxyd — and this script only runs
# what it finds on PATH.
#
# A tool that is not installed is skipped with a warning, so that a workstation
# with shellcheck alone still gets something out of this. MOXY_ANALYZE_REQUIRE=1
# turns every skip into a failure, which is what CI sets: an analyzer that
# quietly stopped being installed would otherwise turn the whole job into a
# green no-op.
#
# Every analyzer runs even when an earlier one failed, so that one invocation
# reports everything there is to fix rather than the first thing.
set -eu
# shellcheck source=scripts/env.sh
. "$(dirname -- "$0")/env.sh"

ROOT="$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)"
WEB_DIR="$ROOT/apps/web"
BIN="$ROOT/bin/moxyd"

status=0

missing() {
	if [ "${MOXY_ANALYZE_REQUIRE:-0}" = "1" ]; then
		echo "$1 is not installed, and MOXY_ANALYZE_REQUIRE=1" >&2
		status=1
	else
		echo "==> skipping $1 (not installed)"
	fi
}

if command -v shellcheck >/dev/null 2>&1; then
	echo "==> shellcheck"
	# -x follows the `. env.sh` of build.sh, check.sh and this script. Without
	# it those three report SC1091 and everything env.sh exports looks
	# undefined. -s sh states the dialect: the scripts are POSIX sh, and the
	# shebang saying `env sh` is not enough for shellcheck to assume it.
	#
	# Two sources, one invocation: scripts/, and the shell that ships in deploy/.
	# One of those files carries no .sh suffix on purpose — moxy-maintenance is an
	# sshd ForceCommand, named by the path it is installed under — so a glob on
	# the suffix would miss it, and naming it here would miss the next one. deploy/
	# is therefore selected on the shebang, which also leaves the Caddyfile, the
	# unit and the sudoers file out of the list on their own.
	(
		cd "$ROOT"
		set -- scripts/*.sh
		for f in deploy/*; do
			[ -f "$f" ] || continue
			head -n 1 "$f" | grep -q '^#!.*sh' || continue
			set -- "$@" "$f"
		done
		shellcheck -x -s sh "$@"
	) || status=1
else
	missing shellcheck
fi

if command -v staticcheck >/dev/null 2>&1; then
	echo "==> staticcheck"
	(cd "$API_DIR" && staticcheck ./...) || status=1
else
	missing staticcheck
fi

if command -v govulncheck >/dev/null 2>&1; then
	echo "==> govulncheck"
	# -mode=binary against the delivered artefact, not `govulncheck ./...`
	# against the sources. moxy has no dependency of its own, so the only thing
	# a vulnerability can come from is the standard library linked into the
	# binary — and the source mode says nothing about which standard library
	# that is. Build it first: scripts/build.sh is what produces this path.
	if [ -f "$BIN" ]; then
		govulncheck -mode=binary "$BIN" || status=1
	else
		echo "$BIN not found: run ./scripts/build.sh first" >&2
		status=1
	fi
else
	missing govulncheck
fi

if command -v npm >/dev/null 2>&1; then
	echo "==> npm audit"
	# Production dependencies only, high and above. The bundle ships inside the
	# image and runs in the operator's browser, which is what makes a runtime
	# advisory worth failing on; a dev-time one would make the gate noisy
	# without making the delivered artefact any safer. This is also the one
	# place that talks to the registry — check-web.sh and build-web.sh keep
	# --no-audit so that an install stays usable offline.
	(cd "$WEB_DIR" && npm audit --omit=dev --audit-level=high) || status=1
else
	missing npm
fi

if [ "$status" -ne 0 ]; then
	echo "==> analysis FAILED" >&2
	exit 1
fi

echo "==> analysis OK"

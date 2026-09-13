#!/usr/bin/env sh
# Run the whole development stack: the mock daemon and the Vite dev server.
#
# The two halves used to need two terminals, and the first one to be forgotten
# was the daemon, leaving a UI whose every request 502s. Here moxyd runs in the
# background and a trap kills it when Vite exits — including on Ctrl-C, which
# reaches both processes but not the one that was never started by this shell.
#
# The UI listens on 127.0.0.1:5173 and proxies /api to the daemon on
# 127.0.0.1:8080 (MOXY_API overrides the target, see apps/web/vite.config.ts).
# No cluster is contacted: the daemon serves the sample data.
set -eu

ROOT="$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)"
SCRIPTS_DIR="$ROOT/scripts"

"$SCRIPTS_DIR/build.sh"
"$SCRIPTS_DIR/web-deps.sh"

echo "==> moxyd -mock"
"$ROOT/bin/moxyd" -mock &
MOCK_PID=$!

# Kill the daemon however this script ends: normal exit, Ctrl-C or a signal.
# Without INT and TERM the background process would outlive the terminal and
# hold port 8080 against the next run.
cleanup() {
	if kill -0 "$MOCK_PID" 2>/dev/null; then
		echo ""
		echo "==> stopping moxyd"
		kill "$MOCK_PID" 2>/dev/null || true
		wait "$MOCK_PID" 2>/dev/null || true
	fi
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

echo "==> vite"
cd "$ROOT/apps/web"
npm run --silent dev

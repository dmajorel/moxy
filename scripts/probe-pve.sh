#!/usr/bin/env sh
# Probe a live Proxmox cluster and report whether moxy's assumptions hold.
#
# moxy's fixtures were written from the documented PVE schema, with no cluster
# reachable from the development environment. Three assumptions were never
# confirmed against a real deployment; this script checks them and prints a
# verdict per assumption. It also reports what the token may actually read,
# which is the other half of "the overview works but the node view is empty".
#
# Read-only: every endpoint it calls needs PVEAuditor at most. Output is in
# English like the rest of the code; the README, in French, is where the
# conclusions are written down.
#
# Requires curl and python3.
#
# Usage:
#   MOXY_SECRET='<uuid>' ./scripts/probe-pve.sh https://node:8006 'moxy@pve!ro' [--insecure]
#
# --insecure may appear at any position.
set -eu

usage() {
	echo "usage: MOXY_SECRET='<uuid>' $0 https://node:8006 'moxy@pve!ro' [--insecure]" >&2
	echo "" >&2
	echo "  --insecure  skip TLS verification (any position)" >&2
	echo "  MOXY_SECRET the token secret, read from the environment only" >&2
	echo "" >&2
	echo "requires: curl, python3" >&2
	exit 2
}

for tool in curl python3; do
	if ! command -v "$tool" >/dev/null 2>&1; then
		echo "$tool is required" >&2
		exit 2
	fi
done

URL=""
TOKEN_ID=""
INSECURE=""
while [ $# -gt 0 ]; do
	case "$1" in
	--insecure)
		INSECURE=1
		;;
	-h | --help)
		usage
		;;
	-*)
		echo "unknown option: $1" >&2
		usage
		;;
	*)
		if [ -z "$URL" ]; then
			URL="$1"
		elif [ -z "$TOKEN_ID" ]; then
			TOKEN_ID="$1"
		else
			echo "unexpected argument: $1" >&2
			usage
		fi
		;;
	esac
	shift
done

if [ -z "$URL" ] || [ -z "$TOKEN_ID" ] || [ -z "${MOXY_SECRET:-}" ]; then
	usage
fi

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT INT TERM

# The secret never reaches curl's argument vector. /proc/<pid>/cmdline is
# world-readable, so a plain `ps -ef` during any of these calls would print it,
# and this script is meant to be run from an admin workstation or a shared
# bastion. A config file read with -K keeps it in a 0600 file inside the 0700
# directory above, for as long as the probe runs.
(
	umask 077
	printf 'header = "Authorization: PVEAPIToken=%s=%s"\n' "$TOKEN_ID" "$MOXY_SECRET" >"$WORK/curlrc"
	if [ -n "$INSECURE" ]; then
		printf 'insecure\n' >>"$WORK/curlrc"
	fi
)

fetch() {
	curl -s --max-time 15 -K "$WORK/curlrc" -w '\n%{http_code}' "$URL/api2/json$1"
}

for path in /cluster/status /cluster/resources /cluster/ha/status/manager_status; do
	name="$(echo "$path" | tr '/' '_')"
	fetch "$path" >"$WORK/$name" || true
done

python3 - "$WORK" <<'PYEOF'
import json, os, sys

work = sys.argv[1]
GREEN, RED, AMBER, OFF = "\033[32m", "\033[31m", "\033[33m", "\033[0m"


def load(name):
    path = os.path.join(work, name)
    if not os.path.exists(path):
        return None, 0
    raw = open(path, encoding="utf-8", errors="replace").read()
    body, _, status = raw.rpartition("\n")
    try:
        status = int(status.strip())
    except ValueError:
        return None, 0
    if status != 200:
        return None, status
    try:
        return json.loads(body).get("data"), status
    except json.JSONDecodeError:
        return None, status


def verdict(ok, label, detail=""):
    mark = f"{GREEN}OK{OFF}" if ok is True else (f"{RED}FAIL{OFF}" if ok is False else f"{AMBER}UNKNOWN{OFF}")
    print(f"  [{mark}] {label}")
    if detail:
        for line in detail.splitlines():
            print(f"         {line}")


def kind(value):
    if isinstance(value, bool):
        return "JSON boolean (true/false)"
    if isinstance(value, int):
        return f"JSON integer ({value})"
    if isinstance(value, str):
        return f"JSON string (\"{value}\")"
    return type(value).__name__


print("\n=== Assumption 3 — boolean serialisation (Flex* types) ===")
status, code = load("_cluster_status")
if status is None:
    verdict(None, f"/cluster/status unreachable (http {code})")
else:
    for entry in status:
        if entry.get("type") == "cluster" and "quorate" in entry:
            verdict(True, f"quorate -> {kind(entry['quorate'])}")
            break
    for entry in status:
        if entry.get("type") == "node" and "online" in entry:
            verdict(True, f"online   -> {kind(entry['online'])}")
            break

resources, code = load("_cluster_resources")
if resources is None:
    verdict(None, f"/cluster/resources unreachable (http {code})")
else:
    seen = {}
    shared_names = {}
    for r in resources:
        for field in ("template", "shared"):
            if field in r and field not in seen:
                seen[field] = r[field]
        for field in ("cpu", "maxcpu", "mem", "maxmem", "uptime"):
            if field in r and not isinstance(r[field], (int, float)) and f"str:{field}" not in seen:
                seen[f"str:{field}"] = r[field]
        if r.get("type") == "storage" and r.get("shared") in (1, True):
            shared_names.setdefault(r.get("storage"), 0)
            shared_names[r["storage"]] += 1
    for field in ("template", "shared"):
        if field in seen:
            verdict(True, f"{field:8s} -> {kind(seen[field])}")
        else:
            verdict(None, f"{field:8s} -> absent from this cluster")
    strings = {k: v for k, v in seen.items() if k.startswith("str:")}
    if strings:
        verdict(True, "numeric fields serialised as strings (the Flex* types earn their keep):",
                "\n".join(f"{k[4:]} = \"{v}\"" for k, v in strings.items()))
    else:
        verdict(True, "no numeric field serialised as a string on this cluster")

    print("\n=== Bonus — shared storage deduplication ===")
    dupes = {k: v for k, v in shared_names.items() if v > 1}
    if dupes:
        verdict(True, "a shared storage does appear once per node:",
                "\n".join(f"{k} x{v}" for k, v in dupes.items()))
    else:
        verdict(None, "no repeated shared storage (no shared storage at all?)")

    # Issue #10: PVE lists a node the token may not audit WITHOUT its figures.
    # The card then has nothing to show for CPU and memory.
    print("\n=== Issue 10 — node figures (Sys.Audit on /nodes) ===")
    node_rows = [r for r in resources if r.get("type") == "node"]
    if not node_rows:
        verdict(False, "no node row in /cluster/resources")
    else:
        blind = sorted(r.get("node", "?") for r in node_rows if "maxcpu" not in r or "maxmem" not in r)
        if blind:
            verdict(False, f"{len(blind)}/{len(node_rows)} node(s) without maxcpu/maxmem:",
                    "\n".join(blind) + "\n"
                    "The token has no Sys.Audit on /nodes/<node>: PVE returns the row without figures.\n"
                    "Grant PVEAuditor on / with propagation, or on /nodes, to the user AND the token.\n"
                    "Careful: a role set on /nodes REPLACES the one inherited from /, it does not add\n"
                    "to it, so that role must carry Sys.Audit itself. See the node ACL section below.")
        else:
            verdict(True, f"all {len(node_rows)} nodes carry cpu/maxcpu/mem/maxmem")

    # Issue #10: every Ceph storage reports the same free space. Show what moxy
    # sees per storage so that the cluster figure can be checked by hand.
    print("\n=== Issue 10 — storages visible to the token ===")
    storages = {}
    for r in resources:
        if r.get("type") != "storage":
            continue
        key = r.get("storage") if r.get("shared") in (1, True) else f"{r.get('node')}/{r.get('storage')}"
        s = storages.setdefault(key, {"rows": 0, "r": r})
        s["rows"] += 1
    if not storages:
        verdict(False, "no storage visible (missing Datastore.Audit?)")
    else:
        lines = [f"{'storage':32s} {'type':8s} {'shared':7s} {'rows':>6s} {'total':>10s} {'free':>10s}  content"]
        avail = {}
        for key, s in sorted(storages.items()):
            r = s["r"]
            total, used = r.get("maxdisk") or 0, r.get("disk") or 0
            free = max(total - used, 0)
            plugin = r.get("plugintype", "?")
            if plugin in ("rbd", "cephfs"):
                avail.setdefault(free, []).append(key)
            tib = lambda b: f"{b / 1024**4:.1f} TiB"
            lines.append(f"{key:32s} {plugin:8s} {'yes' if r.get('shared') in (1, True) else 'no':7s} "
                         f"{s['rows']:6d} {tib(total):>10s} {tib(free):>10s}  {r.get('content', '')}")
        verdict(True, "one storage per line, free = maxdisk - disk:", "\n".join(lines))
        if len(avail) == 1:
            free = next(iter(avail))
            verdict(True, f"every Ceph storage reports the same free space ({free / 1024**4:.1f} TiB):"
                          " one backend, counted once by moxy")
        elif len(avail) > 1:
            verdict(None, "Ceph storages report different free space:",
                    "\n".join(f"{f / 1024**4:.1f} TiB: {', '.join(v)}" for f, v in sorted(avail.items())))

print("\n=== Assumption 1 — manager_status.node_status ===")
ha, code = load("_cluster_ha_status_manager_status")
if ha is None:
    verdict(None, f"/cluster/ha/status/manager_status unreachable (http {code})",
            "403 = the token has no Sys.Audit; 500/404 = no HA manager (cluster without HA).")
else:
    flat = ha.get("node_status") if isinstance(ha, dict) else None
    nested = None
    if isinstance(ha, dict) and isinstance(ha.get("manager_status"), dict):
        nested = ha["manager_status"].get("node_status")
    if flat is None and nested is None:
        verdict(False, "no node_status found", f"keys present: {sorted(ha) if isinstance(ha, dict) else type(ha).__name__}")
    else:
        where = "flat" if flat is not None else "nested in manager_status"
        values = flat if flat is not None else nested
        verdict(True, f"node_status found ({where})")
        known = {"online", "maintenance", "fence", "unknown", "gone"}
        for node, state in sorted(values.items()):
            ok = state in known
            verdict(ok, f"{node} -> \"{state}\"" + ("" if ok else "  <-- VALUE UNKNOWN TO MOXY"))
        if "maintenance" not in values.values():
            verdict(None, "no node in maintenance",
                    "The \"maintenance\" value cannot be observed this way.\n"
                    "See the note in the report: a node has to be drained to confirm it.")
PYEOF

echo ""
echo "=== Assumption 2 and node ACLs — /nodes/<node>/status and apt/update ==="
NODES="$(python3 - "$WORK/_cluster_status" <<'PYEOF'
import json, sys

raw = open(sys.argv[1], encoding="utf-8", errors="replace").read()
body, _, _ = raw.rpartition("\n")
try:
    print(" ".join(e["name"] for e in json.loads(body).get("data", [])
                   if e.get("type") == "node" and e.get("online")))
except Exception:
    pass
PYEOF
)" || NODES=""

if [ -z "$NODES" ]; then
	echo "  [UNKNOWN] no online node identified"
else
	for node in $NODES; do
		# Both calls live under /nodes, and a role set there REPLACES the one
		# inherited from /. A role carrying Sys.Modify alone therefore answers
		# apt/update while wiping Sys.Audit, which is the exact shape that makes
		# the overview keep working while the node and VM views return 403. The
		# two codes are only meaningful read together.
		status_code="$(fetch "/nodes/$node/status" 2>/dev/null | tail -n1 || true)"
		out="$(fetch "/nodes/$node/apt/update" 2>/dev/null || true)"
		apt_code="$(printf '%s' "$out" | tail -n1)"

		if [ "$status_code" = "403" ] && [ "$apt_code" = "200" ]; then
			echo "  [FAIL] $node -> status http 403, apt/update http 200"
			echo "         The role on /nodes carries Sys.Modify but not Sys.Audit, and a role set"
			echo "         on /nodes replaces the one inherited from / instead of adding to it."
			echo "         The overview keeps working; the node and VM views answer 403. Fix with:"
			echo "           pveum role modify MoxyAptCheck --privs \"Sys.Audit,Sys.Modify\""
		elif [ "$status_code" = "403" ]; then
			echo "  [FAIL] $node -> status http 403"
			echo "         The token cannot audit this node: the node and VM views will answer 403."
			echo "         Grant PVEAuditor on / with propagation, to the user AND the token; if a"
			echo "         role is set on /nodes, it must carry Sys.Audit itself."
		elif [ "$status_code" != "200" ]; then
			echo "  [UNKNOWN] $node -> status http ${status_code:-?}"
		else
			echo "  [OK] $node -> status http 200 (Sys.Audit on this node)"
		fi

		if [ "$apt_code" != "200" ]; then
			echo "  [UNKNOWN] $node -> apt/update http ${apt_code:-?} (403 expected when the token has no Sys.Modify on /nodes)"
			continue
		fi
		# Through a file, not a pipe: the interpreter reads its own program from
		# stdin here, so a piped body would never reach json.load. The node name
		# is an argument rather than interpolated into the source — it comes from
		# PVE, and a quote in it would otherwise break the program.
		printf '%s' "$out" | sed '$d' >"$WORK/apt.json"
		python3 - "$node" "$WORK/apt.json" <<'PYEOF'
import json, sys

node = sys.argv[1]
data = json.load(open(sys.argv[2], encoding="utf-8")).get("data") or []
pkgs = [p.get("Package") for p in data]
if not data:
    print(f"  [OK] {node} -> no pending update (nothing to confirm)")
elif "pve-manager" in pkgs:
    v = [p.get("Version") for p in data if p.get("Package") == "pve-manager"][0]
    print(f"  [OK] {node} -> pve-manager present, offered version {v}")
else:
    print(f"  [FAIL] {node} -> {len(data)} pending packages but NO pve-manager")
    print("          the banner will show a count without a version number")
    print(f"          packages: {', '.join(sorted(pkgs)[:8])}")
PYEOF
	done
fi
echo ""

#!/usr/bin/env sh
# Probe a live Proxmox cluster and report whether moxy's assumptions hold.
#
# moxy's fixtures were written from the documented PVE schema, with no cluster
# reachable from the development environment. Three assumptions were never
# confirmed against a real deployment; this script checks them and prints a
# verdict per assumption.
#
# Read-only: every endpoint it calls needs PVEAuditor at most.
#
# Usage:
#   MOXY_SECRET='<uuid>' ./scripts/probe-pve.sh https://node:8006 'moxy@pve!ro' [--insecure]
set -eu

URL="${1:-}"
TOKEN_ID="${2:-}"
INSECURE="${3:-}"

if [ -z "$URL" ] || [ -z "$TOKEN_ID" ] || [ -z "${MOXY_SECRET:-}" ]; then
	echo "usage: MOXY_SECRET='<uuid>' $0 https://node:8006 'moxy@pve!ro' [--insecure]" >&2
	exit 2
fi

CURL_OPTS="-s --max-time 15"
[ "$INSECURE" = "--insecure" ] && CURL_OPTS="$CURL_OPTS -k"

fetch() {
	# shellcheck disable=SC2086
	curl $CURL_OPTS -H "Authorization: PVEAPIToken=$TOKEN_ID=$MOXY_SECRET" \
		-w '\n%{http_code}' "$URL/api2/json$1"
}

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

for path in /cluster/status /cluster/resources /cluster/ha/status/manager_status; do
	name="$(echo "$path" | tr '/' '_')"
	fetch "$path" > "$WORK/$name" || true
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
    mark = f"{GREEN}OK{OFF}" if ok is True else (f"{RED}ECHEC{OFF}" if ok is False else f"{AMBER}INDETERMINE{OFF}")
    print(f"  [{mark}] {label}")
    if detail:
        for line in detail.splitlines():
            print(f"         {line}")


def kind(value):
    if isinstance(value, bool):
        return "booleen JSON (true/false)"
    if isinstance(value, int):
        return f"entier JSON ({value})"
    if isinstance(value, str):
        return f"chaine JSON (\"{value}\")"
    return type(value).__name__


print("\n=== Hypothese 3 — serialisation des booleens (types Flex*) ===")
status, code = load("_cluster_status")
if status is None:
    verdict(None, f"/cluster/status injoignable (http {code})")
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
    verdict(None, f"/cluster/resources injoignable (http {code})")
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
            verdict(None, f"{field:8s} -> absent de ce cluster")
    strings = {k: v for k, v in seen.items() if k.startswith("str:")}
    if strings:
        verdict(True, "champs numeriques serialises en chaine (les Flex* servent) :",
                "\n".join(f"{k[4:]} = \"{v}\"" for k, v in strings.items()))
    else:
        verdict(True, "aucun champ numerique en chaine sur ce cluster")

    print("\n=== Bonus — dedoublonnage des stockages partages ===")
    dupes = {k: v for k, v in shared_names.items() if v > 1}
    if dupes:
        verdict(True, "un stockage partage apparait bien une fois par noeud :",
                "\n".join(f"{k} x{v}" for k, v in dupes.items()))
    else:
        verdict(None, "aucun stockage partage repete (pas de stockage shared ?)")

    # Issue #10: PVE lists a node the token may not audit WITHOUT its figures.
    # The card then has nothing to show for CPU and memory.
    print("\n=== Issue 10 — mesures des noeuds (Sys.Audit sur /nodes) ===")
    node_rows = [r for r in resources if r.get("type") == "node"]
    if not node_rows:
        verdict(False, "aucune ligne node dans /cluster/resources")
    else:
        blind = sorted(r.get("node", "?") for r in node_rows if "maxcpu" not in r or "maxmem" not in r)
        if blind:
            verdict(False, f"{len(blind)}/{len(node_rows)} noeud(s) sans maxcpu/maxmem :",
                    "\n".join(blind) + "\n"
                    "Le token n'a pas Sys.Audit sur /nodes/<noeud> : PVE renvoie la ligne sans mesures.\n"
                    "Poser PVEAuditor sur / avec propagation, ou sur /nodes, pour l'utilisateur ET le token.")
        else:
            verdict(True, f"les {len(node_rows)} noeuds portent cpu/maxcpu/mem/maxmem")

    # Issue #10: every Ceph storage reports the same free space. Show what moxy
    # sees per storage so that the cluster figure can be checked by hand.
    print("\n=== Issue 10 — stockages vus par le token ===")
    storages = {}
    for r in resources:
        if r.get("type") != "storage":
            continue
        key = r.get("storage") if r.get("shared") in (1, True) else f"{r.get('node')}/{r.get('storage')}"
        s = storages.setdefault(key, {"rows": 0, "r": r})
        s["rows"] += 1
    if not storages:
        verdict(False, "aucun stockage visible (Datastore.Audit manquant ?)")
    else:
        lines = [f"{'stockage':32s} {'type':8s} {'partage':7s} {'lignes':>6s} {'total':>10s} {'libre':>10s}  contenu"]
        avail = {}
        for key, s in sorted(storages.items()):
            r = s["r"]
            total, used = r.get("maxdisk") or 0, r.get("disk") or 0
            free = max(total - used, 0)
            plugin = r.get("plugintype", "?")
            if plugin in ("rbd", "cephfs"):
                avail.setdefault(free, []).append(key)
            tib = lambda b: f"{b / 1024**4:.1f} TiB"
            lines.append(f"{key:32s} {plugin:8s} {'oui' if r.get('shared') in (1, True) else 'non':7s} "
                         f"{s['rows']:6d} {tib(total):>10s} {tib(free):>10s}  {r.get('content', '')}")
        verdict(True, "un stockage par ligne, libre = maxdisk - disk :", "\n".join(lines))
        if len(avail) == 1:
            free = next(iter(avail))
            verdict(True, f"les stockages Ceph rapportent tous le meme espace libre ({free / 1024**4:.1f} TiB) :"
                          " un seul backend, compte une fois par moxy")
        elif len(avail) > 1:
            verdict(None, "les stockages Ceph rapportent des espaces libres differents :",
                    "\n".join(f"{f / 1024**4:.1f} TiB : {', '.join(v)}" for f, v in sorted(avail.items())))

print("\n=== Hypothese 1 — manager_status.node_status ===")
ha, code = load("_cluster_ha_status_manager_status")
if ha is None:
    verdict(None, f"/cluster/ha/status/manager_status injoignable (http {code})",
            "403 = le token n'a pas Sys.Audit ; 500/404 = pas de gestionnaire HA (cluster sans HA).")
else:
    flat = ha.get("node_status") if isinstance(ha, dict) else None
    nested = None
    if isinstance(ha, dict) and isinstance(ha.get("manager_status"), dict):
        nested = ha["manager_status"].get("node_status")
    if flat is None and nested is None:
        verdict(False, "aucun node_status trouve", f"cles presentes : {sorted(ha) if isinstance(ha, dict) else type(ha).__name__}")
    else:
        where = "a plat" if flat is not None else "imbrique dans manager_status"
        values = flat if flat is not None else nested
        verdict(True, f"node_status trouve ({where})")
        known = {"online", "maintenance", "fence", "unknown", "gone"}
        for node, state in sorted(values.items()):
            ok = state in known
            verdict(ok, f"{node} -> \"{state}\"" + ("" if ok else "  <-- VALEUR INCONNUE DE MOXY"))
        if "maintenance" not in values.values():
            verdict(None, "aucun noeud en maintenance",
                    "La valeur \"maintenance\" ne peut pas etre observee ainsi.\n"
                    "Voir la note du rapport : il faut drainer un noeud pour la confirmer.")
PYEOF

echo ""
echo "=== Hypothese 2 — pve-manager dans apt/update ==="
NODES=$(python3 -c "
import json,sys
raw=open('$WORK/_cluster_status',encoding='utf-8',errors='replace').read()
body,_,_=raw.rpartition('\n')
try:
    print(' '.join(e['name'] for e in json.loads(body).get('data',[]) if e.get('type')=='node' and e.get('online')))
except Exception:
    pass
" 2>/dev/null || true)

if [ -z "$NODES" ]; then
	echo "  [INDETERMINE] aucun noeud en ligne identifie"
else
	for node in $NODES; do
		out="$(fetch "/nodes/$node/apt/update" || true)"
		code="$(printf '%s' "$out" | tail -n1)"
		if [ "$code" != "200" ]; then
			echo "  [INDETERMINE] $node -> http $code (403 attendu si le token n'a pas Sys.Modify sur /nodes)"
			continue
		fi
		printf '%s' "$out" | sed '$d' | python3 -c "
import json,sys
data=json.load(sys.stdin).get('data') or []
pkgs=[p.get('Package') for p in data]
if not data:
    print('  [OK] $node -> aucune mise a jour en attente (rien a confirmer)')
elif 'pve-manager' in pkgs:
    v=[p.get('Version') for p in data if p.get('Package')=='pve-manager'][0]
    print(f'  [OK] $node -> pve-manager present, version proposee {v}')
else:
    print(f'  [ECHEC] $node -> {len(data)} paquets en attente mais PAS pve-manager')
    print(f'          le bandeau affichera un decompte sans numero de version')
    print(f'          paquets : {\", \".join(sorted(pkgs)[:8])}')
"
	done
fi
echo ""

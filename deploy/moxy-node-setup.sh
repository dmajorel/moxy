#!/usr/bin/env sh
# Prepare one Proxmox VE node so that moxy can put it in and out of maintenance.
#
# Comments are in English like the rest of the tooling; docs/DEPLOIEMENT.md and
# docs/adr/0010-node-maintenance-over-ssh.md explain the reasoning in French.
#
# Run it as root ON EACH NODE of the cluster -- by hand, from Ansible, or with
# `pvesh`-style fan-out; it is idempotent, so replaying it on a node that is
# already prepared changes nothing and says so.
#
#   ./moxy-node-setup.sh --mode ssh-key --pubkey moxy_ed25519.pub [--from 10.0.0.5]
#   ./moxy-node-setup.sh --mode openbao --ca moxy-ca.pub
#   ./moxy-node-setup.sh --remove-authorized-key
#
# What it installs is the same in both modes -- the service account, the
# ForceCommand validator, the sudo grant -- and only the way sshd decides to
# trust moxy differs:
#
#   ssh-key   one public key, pinned in ~moxy/.ssh/authorized_keys, carrying
#             restrict + command="..." and optionally from="...".
#   openbao   no per-node file to keep alive: sshd trusts the SSH CA of the
#             OpenBao mount, and the certificate presented at each connection
#             carries the constraints. See deploy/moxy-openbao-role.md.
#
# The two coexist on a node -- sshd accepts a listed key OR a certificate signed
# by a trusted CA -- which is what makes the switch from one mode to the other
# possible without a window of downtime. So adding the CA NEVER removes the key:
# that is a separate pass, --remove-authorized-key, to be run only once a
# maintenance has been verified end to end through the certificate. Removing the
# key first means discovering a badly scoped OpenBao role with no way left in.
#
# The files it installs live next to this script; copy the whole deploy/
# directory to the node, or point the script at a checkout.
set -eu

SRC_DIR="$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)"
ACCOUNT=moxy
HOME_DIR=/var/lib/moxy
VALIDATOR=/usr/local/sbin/moxy-maintenance
SUDOERS=/etc/sudoers.d/moxy-maintenance
CA_FILE=/etc/ssh/moxy-ca.pub
SSHD_DROPIN=/etc/ssh/sshd_config.d/10-moxy.conf
AUTHORIZED_KEYS="$HOME_DIR/.ssh/authorized_keys"

MODE=
PUBKEY=
CA_SRC=
FROM=
REMOVE_KEY=0
TMP_DIR=

usage() {
	cat >&2 <<'USAGE'
usage:
  moxy-node-setup.sh --mode ssh-key --pubkey FILE [--from ADDRESS]
  moxy-node-setup.sh --mode openbao --ca FILE
  moxy-node-setup.sh --remove-authorized-key

  --mode ssh-key            trust one public key, listed in authorized_keys
  --mode openbao            trust the OpenBao SSH CA, whose certificates carry
                            the constraints (see deploy/moxy-openbao-role.md)
  --pubkey FILE             ssh-key mode: the ed25519 public key of the moxy
                            deployment
  --ca FILE                 openbao mode: the public key of the SSH mount,
                            GET /v1/<mount>/public_key
  --from ADDRESS            ssh-key mode only: restrict the key to the address
                            moxy connects from, when the deployment makes it
                            stable. In openbao mode this is the role's business
                            (source-address), not the node's.
  --remove-authorized-key   remove the key of ssh-key mode and nothing else, the
                            last step of the switch to openbao mode
USAGE
	exit 2
}

die() {
	echo "moxy-node-setup: $*" >&2
	exit 1
}

log() {
	echo "==> $*"
}

cleanup() {
	if [ -n "$TMP_DIR" ]; then
		rm -rf "$TMP_DIR"
	fi
}
trap cleanup EXIT INT TERM

# Install src at dst, and say whether that changed anything. The comparison is
# only there to make a replay readable: `install` would happily rewrite an
# identical file, but then nothing would tell the operator which of ten nodes
# was actually missing something.
install_file() {
	if [ -e "$2" ] && cmp -s "$1" "$2"; then
		log "$2 unchanged"
		return 0
	fi
	install -D -m "$3" -o "${4%:*}" -g "${4#*:}" -- "$1" "$2"
	log "$2 installed"
}

while [ "$#" -gt 0 ]; do
	case "$1" in
	--mode)
		[ "$#" -ge 2 ] || usage
		MODE=$2
		shift 2
		;;
	--pubkey)
		[ "$#" -ge 2 ] || usage
		PUBKEY=$2
		shift 2
		;;
	--ca)
		[ "$#" -ge 2 ] || usage
		CA_SRC=$2
		shift 2
		;;
	--from)
		[ "$#" -ge 2 ] || usage
		FROM=$2
		shift 2
		;;
	--remove-authorized-key)
		REMOVE_KEY=1
		shift
		;;
	-h | --help) usage ;;
	*)
		echo "moxy-node-setup: unknown argument '$1'" >&2
		usage
		;;
	esac
done

# --remove-authorized-key is a pass of its own, and refusing to combine it with
# --mode is what keeps the order of the switch honest: step 1 adds the CA, step
# 4 -- after a maintenance has been verified through it -- removes the key.
if [ "$REMOVE_KEY" -eq 1 ] && [ -n "$MODE" ]; then
	die "--remove-authorized-key is a separate pass: run it alone, once a maintenance has been verified through the certificate"
fi

if [ "$REMOVE_KEY" -eq 0 ]; then
	case "$MODE" in
	ssh-key)
		[ -n "$PUBKEY" ] || die "--mode ssh-key needs --pubkey FILE"
		[ -z "$CA_SRC" ] || die "--ca is only used in openbao mode"
		;;
	openbao)
		[ -n "$CA_SRC" ] || die "--mode openbao needs --ca FILE"
		[ -z "$PUBKEY" ] || die "--pubkey is only used in ssh-key mode"
		[ -z "$FROM" ] || die "--from is only used in ssh-key mode: in openbao mode the address is the role's source-address critical option"
		;;
	"") usage ;;
	*) die "unknown mode '$MODE': expected ssh-key or openbao" ;;
	esac
fi

[ "$(id -u)" -eq 0 ] || die "must run as root"
# The one guard that matters before anything is written: /etc/pve is the cluster
# filesystem, mounted by pve-cluster and by nothing else. No /etc/pve, no node.
[ -d /etc/pve ] || die "/etc/pve not found: this is not a Proxmox VE node"

TMP_DIR="$(mktemp -d)"

if [ "$REMOVE_KEY" -eq 1 ]; then
	if [ -e "$AUTHORIZED_KEYS" ]; then
		rm -f -- "$AUTHORIZED_KEYS"
		log "$AUTHORIZED_KEYS removed"
		log "the key of ssh-key mode no longer opens a session on this node; destroy the pair once every node has had this pass"
	else
		log "$AUTHORIZED_KEYS absent, nothing to remove"
	fi
	exit 0
fi

# a. The service account, common to both modes.
if id -u "$ACCOUNT" >/dev/null 2>&1; then
	log "account $ACCOUNT exists"
	shell="$(getent passwd "$ACCOUNT" | cut -d: -f7)"
	case "$shell" in
	*/nologin | */false) ;;
	*)
		# Not a detail to warn about and move past: "no reachable shell" is
		# the property the whole design rests on, and an account that has one
		# makes every constraint below decorative.
		die "account $ACCOUNT has a usable shell ($shell); fix it with: usermod -s /usr/sbin/nologin $ACCOUNT"
		;;
	esac
else
	useradd --system --create-home --home-dir "$HOME_DIR" --shell /usr/sbin/nologin "$ACCOUNT"
	log "account $ACCOUNT created"
fi
install -d -m 0750 -o "$ACCOUNT" -g "$ACCOUNT" -- "$HOME_DIR"
install -d -m 0700 -o "$ACCOUNT" -g "$ACCOUNT" -- "$HOME_DIR/.ssh"

# c. The ForceCommand validator, root-owned so that the account it constrains
# cannot rewrite it.
[ -f "$SRC_DIR/moxy-maintenance" ] || die "$SRC_DIR/moxy-maintenance not found: copy the whole deploy/ directory to this node"
install_file "$SRC_DIR/moxy-maintenance" "$VALIDATOR" 0755 root:root

# d. The sudo grant. Checked before it is put in place and never after: a
# sudoers file that does not parse is not an inert file, it is a broken sudo for
# every user of this node.
[ -f "$SRC_DIR/moxy-maintenance.sudoers" ] || die "$SRC_DIR/moxy-maintenance.sudoers not found: copy the whole deploy/ directory to this node"
cp -- "$SRC_DIR/moxy-maintenance.sudoers" "$TMP_DIR/sudoers"
chmod 0440 "$TMP_DIR/sudoers"
visudo -c -f "$TMP_DIR/sudoers" >/dev/null || die "$SRC_DIR/moxy-maintenance.sudoers does not parse; nothing was installed"
install_file "$TMP_DIR/sudoers" "$SUDOERS" 0440 root:root

case "$MODE" in
ssh-key)
	# b. The public key, with its constraints.
	[ -f "$PUBKEY" ] || die "$PUBKEY not found"
	# One key per file, and an ed25519 one: the pair is dedicated to this
	# deployment, so there is no reason for the file to hold a second line and
	# no reason to accept a type nobody chose.
	if [ "$(grep -c '[^[:space:]]' -- "$PUBKEY")" -ne 1 ]; then
		die "$PUBKEY must hold exactly one public key"
	fi
	key="$(sed -e 's/[[:space:]]*$//' -- "$PUBKEY" | grep '[^[:space:]]')"
	case "$key" in
	"ssh-ed25519 "*) ;;
	*) die "$PUBKEY is not an ed25519 public key: moxy wants a dedicated ed25519 pair, without a passphrase" ;;
	esac
	# restrict says no to everything at once -- pty, agent, X11, forwarding,
	# ~/.ssh/rc -- and keeps saying no to whatever OpenSSH adds later, which the
	# no-pty,no-port-forwarding,... spelling does not. command= is what turns
	# the requested command into mere data, in $SSH_ORIGINAL_COMMAND.
	options="restrict,command=\"$VALIDATOR\""
	if [ -n "$FROM" ]; then
		case "$FROM" in
		*[!a-zA-Z0-9.:/,*?!-]*) die "--from '$FROM' holds a character an authorized_keys pattern list cannot carry" ;;
		esac
		options="$options,from=\"$FROM\""
	fi
	printf '%s %s\n' "$options" "$key" >"$TMP_DIR/authorized_keys"
	install_file "$TMP_DIR/authorized_keys" "$AUTHORIZED_KEYS" 0600 "$ACCOUNT:$ACCOUNT"
	;;
openbao)
	# b bis. The CA, and nothing per node to keep alive.
	[ -f "$CA_SRC" ] || die "$CA_SRC not found"
	if [ "$(grep -c '[^[:space:]]' -- "$CA_SRC")" -ne 1 ]; then
		die "$CA_SRC must hold exactly one public key (GET /v1/<mount>/public_key)"
	fi
	case "$(sed -e 's/[[:space:]]*$//' -- "$CA_SRC" | grep '[^[:space:]]')" in
	ssh-*) ;;
	*) die "$CA_SRC does not look like an OpenSSH public key" ;;
	esac
	install_file "$CA_SRC" "$CA_FILE" 0644 root:root

	cat >"$TMP_DIR/10-moxy.conf" <<EOF
# moxy: trust the SSH CA of the OpenBao mount that signs its certificates.
#
# The constraints of ssh-key mode do not disappear here, they change owner: the
# role carries force-command, source-address and an empty extension set, so they
# live in one place instead of in a file on every node. See
# deploy/moxy-openbao-role.md.
#
# Installed by deploy/moxy-node-setup.sh --mode openbao.
TrustedUserCAKeys $CA_FILE
EOF
	install_file "$TMP_DIR/10-moxy.conf" "$SSHD_DROPIN" 0644 root:root

	/usr/sbin/sshd -t || die "sshd refuses the configuration with $SSHD_DROPIN in place; it was installed but NOT reloaded"
	# A drop-in that lands where nothing includes it is the silent failure this
	# check exists for: ask sshd what it actually reads rather than guess from
	# an Include line.
	if ! /usr/sbin/sshd -T 2>/dev/null | grep -qi "^trustedusercakeys $CA_FILE$"; then
		die "sshd does not read $SSHD_DROPIN; add 'Include /etc/ssh/sshd_config.d/*.conf' to /etc/ssh/sshd_config, or set TrustedUserCAKeys $CA_FILE there directly"
	fi
	if command -v systemctl >/dev/null 2>&1; then
		unit=
		for candidate in ssh sshd; do
			if systemctl cat "$candidate.service" >/dev/null 2>&1; then
				unit=$candidate
				break
			fi
		done
		if [ -n "$unit" ]; then
			systemctl reload "$unit.service"
			log "$unit.service reloaded"
		else
			log "no ssh unit found: reload sshd by hand"
		fi
	else
		log "no systemctl: reload sshd by hand"
	fi
	log "the authorized_keys of ssh-key mode, if any, is untouched -- remove it with --remove-authorized-key once a maintenance has been verified through the certificate"
	;;
esac

log "node ready ($MODE mode)"
log "next: collect this node's host keys out of band (ssh-keyscan, compared against ssh-keygen -lf on the console) into moxy's known_hosts"

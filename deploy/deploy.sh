#!/usr/bin/env bash
# Build and install the C-460 web UI on an access point over root SSH.
#
#   deploy/deploy.sh <ap-address> [options]
#
# Options:
#   --bootstrap              First install on a freshly unlocked AP: create the
#                            OpenConfig API user (signs in with the factory
#                            admin/admin once) and provision hostname/country.
#   --country CC             Regulatory country for --bootstrap, e.g. DE.
#   --gnmi-credentials FILE  JSON {"username": "...", "password": "..."} of an
#                            existing OpenConfig API user (instead of --bootstrap).
#   --site-name NAME         Label shown in the UI (default: none, the hostname is shown).
#   --vlan-names FILE        JSON object mapping VLAN ids to names, e.g. {"10": "Office"}.
#   --set-password           Set the web UI login again even if one exists.
#   --username NAME          Login name for the web UI (default: config).
#   --ssh-key FILE           SSH identity for root@<ap> (default: ssh config / agent).
#   --known-hosts FILE       Pinned known_hosts file for the AP.
#   --no-build               Install the existing build/c460-webui.
#   --check                  Only run the firmware trust pre-check (deploy/overlay-check.sh).
#   --uninstall              Stop and remove the web UI from the AP.
#
# Set C460_UI_PASSWORD (and optionally C460_UI_USERNAME) to skip the prompts.
set -euo pipefail

usage() { sed -n '2,/^set -euo/{/^set -euo/!p;}' "$0" | sed 's/^# \{0,1\}//'; exit "${1:-1}"; }

[ $# -ge 1 ] || usage
case "$1" in -h | --help) usage 0 ;; esac
HOST=$1
shift
GNMI_CREDS="" BOOTSTRAP=0 COUNTRY="" SITE_NAME="" VLAN_NAMES="" SET_PASSWORD=0 BUILD=1 UNINSTALL=0 CHECK_ONLY=0 UI_USER=${C460_UI_USERNAME:-}
SSH_OPTS=(-o BatchMode=yes -o ConnectTimeout=10)
while [ $# -gt 0 ]; do
	case "$1" in
	--gnmi-credentials) GNMI_CREDS=$2; shift ;;
	--bootstrap) BOOTSTRAP=1 ;;
	--country) COUNTRY=$2; shift ;;
	--site-name) SITE_NAME=$2; shift ;;
	--vlan-names) VLAN_NAMES=$2; shift ;;
	--set-password) SET_PASSWORD=1 ;;
	--username) UI_USER=$2; SET_PASSWORD=1; shift ;;
	--ssh-key) SSH_OPTS+=(-i "$2" -o IdentitiesOnly=yes); shift ;;
	--known-hosts) SSH_OPTS+=(-o StrictHostKeyChecking=yes -o "UserKnownHostsFile=$2"); shift ;;
	--no-build) BUILD=0 ;;
	--uninstall) UNINSTALL=1 ;;
	--check) CHECK_ONLY=1 ;;
	-h | --help) usage 0 ;;
	*) echo "unknown option: $1" >&2; usage ;;
	esac
	shift
done

ROOT=$(cd "$(dirname "$0")/.." && pwd)
DIR=/opt/c460-webui
# The firmware wipes the writable layer at boot if it finds regular files in
# protected directories (/etc, /usr, /opt/init.d, ...), so everything we
# install lives in $DIR and the init/boot entries are only symlinks.
# procd names the service after the basename one symlink hop away, so the real
# script must also be called c460-webui (boot: S0900… -> /opt/init.d/c460-webui,
# manual: /opt/init.d/c460-webui -> $DIR/init/c460-webui); otherwise two
# services fight over port 80.
INIT=/opt/init.d/c460-webui
BOOTLINK=/etc/rc.d/S0900c460-webui
ap() { ssh "${SSH_OPTS[@]}" "root@$HOST" "$@" 2>&1 | sed '/^-\{10,\}$/d; /^ \[AP\]/d; /^ Network Interface/d'; return "${PIPESTATUS[0]}"; }
step() { printf '\033[1m==> %s\033[0m\n' "$*"; }

step "Checking $HOST"
info=$(ap 'uname -m; sed -n "s/.*Model *: *\[\(.*\)\]/\1/p" /opt/banner 2>/dev/null; test -f /opt/openconfig/cert/agent.crt && echo agent-ok')
echo "$info" | grep -qx aarch64 || { echo "Not an ARM64 AP or root SSH failed:" >&2; echo "$info" >&2; exit 1; }
model=$(echo "$info" | sed -n 2p)
case "$model" in C-460*) ;; *) echo "Warning: model is '${model:-unknown}', this tool targets the C-460." >&2 ;; esac
echo "$info" | grep -qx agent-ok || { echo "OpenConfig agent certificate not found; enable OpenConfig mode first (see README)." >&2; exit 1; }

# Runs deploy/overlay-check.sh on the AP; fails when the next boot would wipe the writable layer.
trust_check() {
	step "Checking the firmware's boot-time trust rules"
	if ! ap 'sh -s' <"$ROOT/deploy/overlay-check.sh"; then
		echo "Do NOT reboot $HOST until the files above are removed: the AP would wipe its writable layer." >&2
		return 1
	fi
}

if [ "$CHECK_ONLY" = 1 ]; then
	trust_check
	exit $?
fi

if [ "$UNINSTALL" = 1 ]; then
	step "Removing the web UI"
	ap ". /etc/profile >/dev/null 2>&1; $INIT stop 2>/dev/null; rm -f $BOOTLINK $INIT; rm -rf $DIR; echo removed"
	trust_check
	exit $?
fi

if [ "$BUILD" = 1 ]; then
	step "Building"
	make -C "$ROOT" build
fi
[ -x "$ROOT/build/c460-webui" ] || { echo "build/c460-webui is missing; run make build" >&2; exit 1; }

has_config=$(ap "test -f $DIR/config.json && echo yes || echo no" | tail -1)
if [ "$has_config" != yes ] && [ -z "$GNMI_CREDS" ] && [ "$BOOTSTRAP" != 1 ]; then
	echo "First install on $HOST: use --bootstrap --country CC (new AP) or --gnmi-credentials FILE." >&2
	exit 1
fi

step "Uploading binary"
ap "mkdir -p $DIR && chmod 700 $DIR && cat > $DIR/c460-webui.new && chmod 700 $DIR/c460-webui.new && mv $DIR/c460-webui.new $DIR/c460-webui" <"$ROOT/build/c460-webui"
ap "mkdir -p $DIR/init && cat > $DIR/init/c460-webui && chmod 755 $DIR/init/c460-webui && rm -f $INIT $DIR/c460-webui.init && ln -s $DIR/init/c460-webui $INIT && ln -sfn $INIT $BOOTLINK" <"$ROOT/deploy/c460-webui.init"

if [ "$BOOTSTRAP" = 1 ]; then
	step "Creating the OpenConfig API user and provisioning the AP"
	[[ -z "$COUNTRY" || "$COUNTRY" =~ ^[A-Za-z]{2}$ ]] || { echo "--country needs a two-letter code such as DE" >&2; exit 1; }
	ap "env -u LD_PRELOAD $DIR/c460-webui -config $DIR/config.json -bootstrap ${COUNTRY:+-country $COUNTRY}" || { echo "Bootstrap failed; see the message above." >&2; exit 1; }
fi

if [ -n "$GNMI_CREDS" ] || [ -n "$SITE_NAME" ] || [ -n "$VLAN_NAMES" ]; then
	step "Writing configuration"
	current=$(ap "cat $DIR/config.json 2>/dev/null || echo '{}'")
	python3 - "$GNMI_CREDS" "$SITE_NAME" "$VLAN_NAMES" "$current" >"$ROOT/build/.config.json" <<'PY'
import json, sys
creds_file, site, vlans_file, current = sys.argv[1:5]
cfg = json.loads(current or "{}")
cfg.setdefault("listen", ":80")
cfg.setdefault("pollSeconds", 5)
cfg.setdefault("authFile", "/opt/c460-webui/auth.json")
if creds_file:
    creds = json.load(open(creds_file))
    cfg.setdefault("gnmi", {}).update({"username": creds["username"], "password": creds["password"]})
if site:
    cfg["siteName"] = site
if vlans_file:
    cfg["vlanNames"] = {str(k): str(v) for k, v in json.load(open(vlans_file)).items()}
print(json.dumps(cfg, indent=2))
PY
	chmod 600 "$ROOT/build/.config.json"
	ap "umask 077; cat > $DIR/config.json" <"$ROOT/build/.config.json"
	rm -f "$ROOT/build/.config.json"
fi

has_auth=$(ap "test -f $DIR/auth.json && echo yes || echo no" | tail -1)
if [ "$has_auth" != yes ] || [ "$SET_PASSWORD" = 1 ]; then
	step "Set the web UI administrator login"
	if [ -z "$UI_USER" ]; then
		if [ -n "${C460_UI_PASSWORD:-}" ]; then UI_USER=config; else read -r -p "Username [config]: " UI_USER; UI_USER=${UI_USER:-config}; fi
	fi
	[[ "$UI_USER" =~ ^[A-Za-z0-9._-]{1,32}$ ]] || { echo "Username: 1-32 letters, digits, '.', '_' or '-'." >&2; exit 1; }
	pw1=${C460_UI_PASSWORD:-} pw2=${C460_UI_PASSWORD:-}
	while [ -z "$pw1" ] || [ "$pw1" != "$pw2" ] || [ ${#pw1} -lt 6 ]; do
		read -r -s -p "New password (min. 6 characters): " pw1; echo
		read -r -s -p "Repeat: " pw2; echo
		[ "$pw1" = "$pw2" ] && [ ${#pw1} -ge 6 ] || echo "Passwords differ or are shorter than 6 characters, try again."
	done
	printf '%s\n' "$pw1" | ap "$DIR/c460-webui -config $DIR/config.json -set-password -username '$UI_USER'"
	unset pw1 pw2
fi

step "Starting service"
ap ". /etc/profile >/dev/null 2>&1; $INIT stop >/dev/null 2>&1; $INIT start; sleep 2; pidof c460-webui >/dev/null && echo running || echo NOT running"
code=$(curl -s -o /dev/null -w '%{http_code}' --max-time 5 "http://$HOST/api/session" || true)
if [ "$code" != 200 ]; then
	echo "The service did not answer on http://$HOST/ (HTTP $code). Check: ssh root@$HOST logread | grep c460-webui" >&2
	exit 1
fi
trust_check || exit 1
step "Done: http://$HOST/"

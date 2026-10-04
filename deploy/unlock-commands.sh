#!/usr/bin/env bash
# Prints the commands that prepare a new C-460 for this web interface
# (README steps 2-4), with your SSH public key filled in. Paste them into the
# AP's command line (ssh config@<ap>, password config, or the serial console).
#
#   deploy/unlock-commands.sh [public-key-file]   (default ~/.ssh/id_ed25519.pub)
set -euo pipefail
KEY_FILE=${1:-$HOME/.ssh/id_ed25519.pub}
if [ ! -f "$KEY_FILE" ]; then
	echo "No public key at $KEY_FILE. Create one with: ssh-keygen -t ed25519" >&2
	exit 1
fi
KEY=$(tr -d '\r\n' <"$KEY_FILE")
case "$KEY" in
ssh-ed25519\ * | ssh-rsa\ * | ecdsa-sha2-*) ;;
*) echo "$KEY_FILE does not look like an SSH public key." >&2; exit 1 ;;
esac
# The key is placed inside single quotes on the AP; refuse anything that could break out.
case "$KEY" in *\'* | *\"* | *\;* | *\\*) echo "Unexpected characters in the key; add it by hand." >&2; exit 1 ;; esac

cat <<CMDS
# --- Step 2: keep the AP away from the cloud ---------------------------------
server discovery method ipdns pri 127.0.0.1 sec 127.0.0.1

# --- Step 3: enable local configuration (OpenConfig) and save it -------------
radartool radio 0 params ";. /opt/ap/configparser; cfg_set oc_enabled 1 /opt/sensor/sensor.conf; /opt/init.d/handle_openconfig_mode INIT;"
radartool radio 0 params ";/opt/sensor/scripts/encrypt_secret.sh -i /opt/sensor/sensor.conf -o /tmp/sensor-oc.enc && test -s /tmp/sensor-oc.enc && mv /tmp/sensor-oc.enc /opt/sensor/sensor.conf.enc;"

# --- Step 4: allow root login with your key -----------------------------------
radartool radio 0 params ";mkdir -p /root/.ssh; echo '$KEY' >> /root/.ssh/authorized_keys; chmod 700 /root/.ssh; chmod 600 /root/.ssh/authorized_keys;"

# --- Check -------------------------------------------------------------------
show server discovery
show openconfig mode
CMDS

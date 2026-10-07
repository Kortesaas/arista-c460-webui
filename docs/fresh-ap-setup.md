# From a factory-fresh AP to a local WebUI

**A practical setup guide for the Arista C-460.** Prepare the AP once, deploy the existing software over SSH, and manage it through its own browser interface and API.

> [!NOTE]
> This procedure was verified on a fresh **C-460 running firmware 18.2.0-32**, including a full reboot, root SSH, browser sign-in, API access and persistence of the local configuration. Other firmware versions are untested.

**The path:** serial console → disable cloud management → enable local mode → root SSH → deploy → configure the network → verify after reboot.

## Contents

- [1. Prepare your computer and connections](#1-prepare-your-computer-and-connections)
- [2. Sign in and identify the AP](#2-sign-in-and-identify-the-ap)
- [3. Disable Arista cloud management](#3-disable-arista-cloud-management)
- [4. Enable and save local configuration](#4-enable-and-save-local-configuration)
- [5. Set up persistent root SSH](#5-set-up-persistent-root-ssh)
- [6. Verify SSH and build the software](#6-verify-ssh-and-build-the-software)
- [7. Deploy the WebUI and API](#7-deploy-the-webui-and-api)
- [8. Configure the management network](#8-configure-the-management-network)
- [9. Verify everything after a full reboot](#9-verify-everything-after-a-full-reboot)
- [Troubleshooting](#troubleshooting)
- [Deploying the next AP and future updates](#deploying-the-next-ap-and-future-updates)

---

## 1. Prepare your computer and connections

### What you need

| Item | Requirement |
|---|---|
| Access point | Arista C-460, firmware **18.2.0-32** |
| Power | Suitable DC power, or **802.3at PoE+** or better; 802.3af reduces the available radio power |
| Recovery access | A serial console connection; keep it available until the final checks pass |
| Computer tools | Git, Bash, SSH, Python 3, curl, make, **Go ≥ 1.26** and **Node.js ≥ 22.12** |
| SSH identity | An existing SSH key, preferably Ed25519 |
| Network | Ethernet access to the AP, either directly or through a switch |

Connect power, Ethernet and serial. Set the serial terminal to **115200 baud, 8 data bits, no parity, 1 stop bit, no flow control**. Allow about three minutes for the AP to finish booting.

A router, DHCP server and internet connection are **not required on the AP's network** for this procedure. Your computer needs the source code and build dependencies; if they are already available, deployment can proceed offline.

**On your computer**, get the repository:

```bash
git clone https://github.com/Kortesaas/arista-c460-webui.git
cd arista-c460-webui
```

For an existing checkout, use `git pull --ff-only` instead. Use the current version: fresh-AP provisioning, persistent SSH enablement and initial radio configuration are handled by the deployer.

Check that you have a public key:

```bash
cat ~/.ssh/id_ed25519.pub
```

If you do not have an SSH key yet, create one with `ssh-keygen -t ed25519`. Keep the private key on your computer; only the `.pub` key is copied to the AP.

## 2. Sign in and identify the AP

**In the serial console**, sign in with the factory CLI account:

| Field | Factory value |
|---|---|
| Username | `config` |
| Password | `config` |

You should reach the vendor prompt:

```text
[config]$
```

Read the model, MAC address and firmware:

```text
show device info
```

Confirm that you are configuring the intended AP and that it is running the supported firmware. Repeat this identification on every new unit.

> [!TIP]
> Commands marked **AP CLI** belong at `[config]$`. Commands marked **computer** belong in your computer's shell. An ordinary root shell is available only after the SSH preparation below.

## 3. Disable Arista cloud management

Do this **before connecting the AP to a network with internet access**. Cloud management can disable local mode and replace the local configuration.

**AP CLI:**

```text
server discovery method ipdns pri 127.0.0.1 sec 127.0.0.1
show server discovery
show device status
```

Confirm these values:

| Setting | Expected value |
|---|---|
| Discovery mode | `IP/DNS` |
| Server selection | `CLI` |
| Primary server | `127.0.0.1` |
| Secondary server | `127.0.0.1` |
| Connection state | `Not connected` |

The root SSH preparation in step 5 also points the cached `server_addr` at loopback. This addresses both the configured discovery destinations and the cached destination used in the verified setup.

> [!CAUTION]
> Do not run `default server discovery`: it restores cloud discovery. These settings disable the cloud management connection; they do not disable normal client internet access.

## 4. Enable and save local configuration

The radio tool can execute a root shell command from the authenticated vendor CLI. First verify that access:

**AP CLI:**

```text
radartool radio 0 params ";id;"
```

The result should include **`uid=0`**. The username may appear as `config` because `config` and `root` share UID 0 on this firmware.

Enable OpenConfig, then save the encrypted configuration used at boot:

```text
radartool radio 0 params ";. /opt/ap/configparser; cfg_set oc_enabled 1 /opt/sensor/sensor.conf; /opt/init.d/handle_openconfig_mode INIT;"
radartool radio 0 params ";/opt/sensor/scripts/encrypt_secret.sh -i /opt/sensor/sensor.conf -o /tmp/sensor-oc.enc && test -s /tmp/sensor-oc.enc && mv /tmp/sensor-oc.enc /opt/sensor/sensor.conf.enc;"
show openconfig mode
```

Expected result:

```text
GRPC Mode      : openconfig
Openconfig Mode: Enabled
```

**Both commands matter:** enabling the running service alone does not complete the persistent setup.

## 5. Set up persistent root SSH

This uses the AP's existing SSH daemon. It adds your public key under `/opt/root-ssh/` and a root-only authentication block to the persistent template `/opt/sshd_config`. It keeps the root password locked and preserves the factory `config` account.

The AP generates `/tmp/sshd_config` from the persistent template when SSH starts. Editing only `/tmp/sshd_config` would lose the change on restart. The root block ends with **`Match all`** so that the firmware can append its normal global settings.

### Generate short commands on your computer

Run the following **on your computer**. It reads your public key and prints the commands to paste into the AP CLI. Change the public-key path if needed.

The script is sent in short chunks because a long command can exceed the console's input limit. The AP does not provide a `base64` command on the tested firmware, so its existing Python interpreter decodes it with `-B` to avoid creating bytecode files.

```bash
python3 - <<'PY'
import base64
from pathlib import Path

parts = (Path.home() / '.ssh/id_ed25519.pub').read_text().split()
if len(parts) < 2 or parts[0] != 'ssh-ed25519':
    raise SystemExit('Use an Ed25519 public key for this example.')
base64.b64decode(parts[1], validate=True)
key = ' '.join(parts[:2])

script = r'''set -eu
. /etc/profile >/dev/null 2>&1
umask 077
mkdir -p /opt/root-ssh/before-setup
chmod 700 /opt/root-ssh /opt/root-ssh/before-setup
test ! -e /opt/root-ssh/before-setup/template
cp -p /opt/sshd_config /opt/root-ssh/before-setup/template
cp -p /opt/sensor/discovery.conf /opt/root-ssh/before-setup/discovery.conf
printf '%s\n' 'PUBLIC_KEY' > /opt/root-ssh/authorized_keys
chmod 600 /opt/root-ssh/authorized_keys
cp /opt/sshd_config /opt/root-ssh/sshd_config.new
cat >> /opt/root-ssh/sshd_config.new <<'EOF'

Match User root
    PermitRootLogin prohibit-password
    PubkeyAuthentication yes
    AuthenticationMethods publickey
    PasswordAuthentication no
    KbdInteractiveAuthentication no
    AuthorizedKeysFile /opt/root-ssh/authorized_keys
Match all
EOF
. /opt/ap/configparser
cfg_set ssh_enabled true /opt/openconfig/system.conf
/etc/init.d/opensshd start
/bin/sshd -t -f /opt/root-ssh/sshd_config.new -h /opt/ssh_host_rsa_key
mv /opt/root-ssh/sshd_config.new /opt/sshd_config
cfg_set server_addr 127.0.0.1 /opt/sensor/discovery.conf
/etc/init.d/opensshd restart
echo ROOT_SSH_CONFIGURED
'''.replace('PUBLIC_KEY', key)

encoded = base64.b64encode(script.encode()).decode()
print('radartool radio 0 params ";umask 077; mkdir -p /opt/root-ssh; chmod 700 /opt/root-ssh; : > /opt/root-ssh/setup.b64;"')
for offset in range(0, len(encoded), 160):
    chunk = encoded[offset:offset + 160]
    print(f'radartool radio 0 params ";printf {chunk} >> /opt/root-ssh/setup.b64;"')
print(r'''radartool radio 0 params ";. /etc/profile >/dev/null 2>&1; python3 -B -c 'import base64;print(base64.b64decode(open(\"/opt/root-ssh/setup.b64\").read()).decode())' | sh;"''')
PY
```

### Apply them in the AP CLI

Paste the generated lines **one at a time**, waiting for `[config]$` to return after each line. The last command should print:

```text
ROOT_SSH_CONFIGURED
```

This preparation runs once per fresh AP. If the backup already exists, the script stops rather than overwriting it. Once root SSH works, proceed to deployment instead of repeating this preparation.

Starting the native SSH service before validating the new template also creates its temporary privilege-separation directory. This is needed on units where SSH was previously disabled.

Read the AP's public host key and its current addresses through the trusted serial connection:

```text
radartool radio 0 params ";cat /opt/ssh_host_rsa_key.pub; ip -4 addr show br0; ip -6 addr show br0;"
```

Use the IPv4 address if the AP received one from DHCP. Without DHCP, use its **IPv6 link-local address**, appending `%` and your computer's connected Ethernet interface name or index. This allows deployment before a permanent IPv4 address has been configured.

## 6. Verify SSH and build the software

**On your computer**, set the values for this AP. The addresses and name below are examples; replace them with your own.

```bash
C460_AP_HOST='192.168.1.40'       # Current reachable address; IPv6 link-local also works
C460_AP_IP='192.168.1.40'         # Intended permanent IPv4 address
C460_AP_NAME='Access Point 1'
C460_COUNTRY='DE'                # Your actual regulatory country
C460_SSH_KEY="$HOME/.ssh/id_ed25519"
C460_KNOWN_HOSTS="$HOME/.ssh/c460-ap1-known-hosts"
```

Copy the **key type and base64 data** from `/opt/ssh_host_rsa_key.pub` into the next variable. Use the host key read from this AP's serial console, not a key from another AP.

```bash
C460_HOST_KEY='ssh-rsa PASTE_THE_PUBLIC_HOST_KEY_DATA_FROM_SERIAL'
umask 077
printf '%s %s\n' "$C460_AP_HOST,$C460_AP_IP" "$C460_HOST_KEY" > "$C460_KNOWN_HOSTS"
chmod 600 "$C460_KNOWN_HOSTS"
ssh-keygen -lf "$C460_KNOWN_HOSTS"

ssh -i "$C460_SSH_KEY" -o IdentitiesOnly=yes \
  -o StrictHostKeyChecking=yes -o "UserKnownHostsFile=$C460_KNOWN_HOSTS" \
  "root@$C460_AP_HOST" 'id; printf "%s\n" "$HOME"'
```

Expected: UID 0 and home directory `/root`, without a password prompt. A UID display of `config` is normal on this firmware.

Remove the temporary transfer file after SSH works:

```bash
ssh -i "$C460_SSH_KEY" -o IdentitiesOnly=yes \
  -o StrictHostKeyChecking=yes -o "UserKnownHostsFile=$C460_KNOWN_HOSTS" \
  "root@$C460_AP_HOST" 'rm -f /opt/root-ssh/setup.b64'
```

**From the repository directory on your computer**, build once:

```bash
make build
```

This creates `build/c460-webui`, a single ARM64 binary containing the backend and browser assets. You can reuse it on subsequent prepared APs.

## 7. Deploy the WebUI and API

**On your computer, from the repository directory:**

```bash
C460_UI_USERNAME=config C460_UI_PASSWORD=config \
  bash deploy/deploy.sh "$C460_AP_HOST" \
  --no-build --bootstrap --country "$C460_COUNTRY" \
  --site-name "$C460_AP_NAME" \
  --ssh-key "$C460_SSH_KEY" --known-hosts "$C460_KNOWN_HOSTS"
```

The login **`config` / `config`** matches the tested initial setup. Omit `C460_UI_PASSWORD=config` if you want the deployer to prompt for a different password.

The deployer checks cloud discovery and boot trust, uploads the binary, provisions the regulatory country, creates a separate private OpenConfig API account, enables native SSH, initializes the three radio controls if none exist, sets the WebUI login and starts the service. Existing radio settings are preserved.

| Component | Location on the AP |
|---|---|
| Binary and application files | `/opt/c460-webui/` |
| Local API connection configuration | `/opt/c460-webui/config.json` |
| WebUI account data | `/opt/c460-webui/auth.json` |
| Service entry | `/opt/init.d/c460-webui` → symlink into the application directory |
| Boot entry | `/etc/rc.d/S0900c460-webui` → symlink to the service entry |

There is no package manager or separate release bundle involved. The existing deployment script copies the software over root SSH.

### If the country change restarts the AP

A country change can restart the radios or reboot the whole unit. The deployment connection may close before setup finishes. Wait until the AP has finished booting, then **run the same deployment command again**.

Replacement API credentials are saved before provisioning in `/opt/c460-webui/config.json.bootstrap`, or in `config.json` once verified. Keep those files if setup is interrupted; the retry reuses the saved credentials.

If SSH has not yet been enabled through OpenConfig when the restart occurs, restore it through the serial console and retry:

**AP CLI:**

```text
radartool radio 0 params ";. /opt/ap/configparser; cfg_set ssh_enabled true /opt/openconfig/system.conf; /etc/init.d/opensshd restart;"
```

Successful deployment ends with a trust-check result and the WebUI address:

```text
OK: the writable layer passes the firmware's boot-time trust checks
==> Done: http://<ap-address>/
```

> [!IMPORTANT]
> The firmware checks the writable layer during boot and can wipe it if regular files were added to protected directories. Use the deployer's existing paths and symlinks. If the trust check reports a problem, resolve it before rebooting.

## 8. Configure the management network

Open the WebUI and sign in with the login chosen during deployment.

If you deployed over IPv6 link-local and cannot open that address in your browser, use an SSH forward **on your computer**:

```bash
ssh -i "$C460_SSH_KEY" -o IdentitiesOnly=yes \
  -o StrictHostKeyChecking=yes -o "UserKnownHostsFile=$C460_KNOWN_HOSTS" \
  -N -L 127.0.0.1:8082:127.0.0.1:80 "root@$C460_AP_HOST"
```

Leave that terminal open and visit `http://127.0.0.1:8082/`.

### Choose DHCP or a static address

Under **Network**, edit the management settings:

| Setting | What to enter |
|---|---|
| Address mode | DHCP, or static if you want a fixed address on the AP |
| IPv4 address | An unused address on your management subnet |
| Netmask | The mask for that subnet |
| Gateway | Your router's address |
| DNS | Your router or another reachable DNS server |
| Management VLAN | Untagged, unless your switch expects a specific tagged VLAN |

For example: `192.168.1.40`, mask `255.255.255.0`, gateway and DNS `192.168.1.1`. A gateway that will be connected later can be saved now. Until it is connected, expect local management access only.

**Saving these settings stages them for the next reboot.** Use the WebUI's restart action to apply them after its trust check passes. Your computer must be able to reach the selected management subnet afterward.

### The API uses the same management settings

The verified setup saved the address through `PUT /api/management`, then restarted through `POST /api/reboot`. The WebUI exposes these same operations. For automation, the versioned equivalents are:

```text
POST /api/v1/login
PUT  /api/v1/management
POST /api/v1/reboot
```

Sign in with the WebUI account, retain the returned session cookie, and send JSON requests. A static-address request has this form:

```json
{
  "commVlan": "untagged",
  "mode": "static",
  "ipv4": "192.168.1.40",
  "netmask": "255.255.255.0",
  "gateway": "192.168.1.1",
  "dns": ["192.168.1.1"],
  "dnsSearch": ""
}
```

The management response includes `rebootRequired: true` when a restart is needed. Send `{}` to the reboot endpoint. See the [API documentation](../README.md#local-api-for-integrations) for authentication and other endpoints.

## 9. Verify everything after a full reboot

Wait for the AP to finish booting. Open its permanent address and sign in again.

| Check | Expected result |
|---|---|
| WebUI | Dashboard loads and shows the chosen AP name and management IP |
| Root SSH | Your key still opens a root shell |
| Management settings | Correct address, mask, gateway and DNS; no pending restart |
| Local mode | `show openconfig mode` still reports enabled |
| Arista discovery | Both servers remain loopback; `show device status` reports `Not connected` |
| Cached destination | `server_addr` is `127.0.0.1` |
| Radio controls | Three entries: **2.4, 5 and 6 GHz** |
| Boot trust | The check still passes |

**AP CLI**, through serial or the factory `config` SSH account:

```text
show server discovery
show device status
show openconfig mode
radartool radio 0 params ";sed -n '/server_addr/p' /opt/sensor/discovery.conf;"
```

**On your computer**, verify root SSH at the permanent IPv4 address and run the deployer's read-only trust check:

```bash
ssh -i "$C460_SSH_KEY" -o IdentitiesOnly=yes \
  -o StrictHostKeyChecking=yes -o "UserKnownHostsFile=$C460_KNOWN_HOSTS" \
  "root@$C460_AP_IP" 'id; printf "%s\n" "$HOME"'

bash deploy/deploy.sh "$C460_AP_IP" --check \
  --ssh-key "$C460_SSH_KEY" --known-hosts "$C460_KNOWN_HOSTS"
```

For API verification, log in and check `/api/v1/session` and `/api/v1/state`. The session should report administrator access; the state should show the correct device, management settings and three radios.

### Start using the AP

- **Wireless networks → Add network:** create your SSIDs. A fresh deployment creates no SSIDs automatically.
- **Radios:** choose channels, widths and power for your environment.
- **Network → Time synchronisation:** choose a reachable NTP server and time zone once the network is connected.
- **System → Backup and restore:** download a backup after configuring the AP.

The three radio controls are available after setup. Broadcasting, client connectivity and throughput require an SSID and a connected test device; they are separate checks from this commissioning procedure.

---

## Troubleshooting

| Symptom | What to check |
|---|---|
| Console shows boot messages rather than a prompt | Wait for startup to finish; verify 115200 baud, 8N1 and no flow control. |
| Long serial command is rejected or truncated | Use the short generated commands in step 5, one line at a time. |
| `base64: not found` on the AP | Use the Python decoding command from step 5. No additional AP package is needed. |
| Root SSH rejects the key | Check the copied public key, permissions, `/opt/sshd_config` and the root `AuthorizedKeysFile` block. Keep the root password locked. |
| SSH is refused after the country restart | Wait for startup; if necessary, enable the native SSH flag through the serial command in step 7, then retry deployment. |
| Bootstrap reports `provision-aps ... not found` | Update the repository and rebuild. The current bootstrap handles the empty configuration of a fresh AP. |
| Dashboard has no radio controls | Update and rebuild, then repeat deployment with `--bootstrap`. The current bootstrap initializes an empty radio configuration. |
| Deployment stops because cloud discovery is active | Repeat step 3 and confirm both loopback destinations. |
| Trust check reports protected files | Resolve the listed files before a restart; follow the [boot-trust explanation](../README.md#the-firmwares-boot-time-trust-check). |
| Saved management address is not active yet | It takes effect after a full reboot. Check reachability of the new address and management VLAN. |
| Clock is not synchronised | Configure NTP and ensure the configured server can be reached. A disconnected gateway cannot provide access to remote time servers. |

## Deploying the next AP and future updates

**For another fresh AP**, repeat its serial preparation and host-key verification. Choose its own address, name, known-hosts file and regulatory country. Reuse the existing `build/c460-webui` binary with `--no-build --bootstrap`.

**For a prepared AP**, update the repository, rebuild, and deploy without `--bootstrap`:

```bash
git pull --ff-only
make build
bash deploy/deploy.sh "$C460_AP_IP" --no-build \
  --ssh-key "$C460_SSH_KEY" --known-hosts "$C460_KNOWN_HOSTS"
```

The deployer keeps the AP's configuration and login. Each AP has its own private OpenConfig credentials; do not copy those files between devices or commit them to GitHub.

[Back to the README](../README.md)

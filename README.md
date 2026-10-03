# arista-c460-webui

A local, self-hosted web interface for the **Arista C-460** Wi-Fi 7 access point. It runs on the access point itself and lets you manage it from a browser at `http://<ap-ip>/`, like the built-in web UI of a typical standalone access point. No controller and no cloud service are needed.

The UI carries a parody brand, **ARRR-ISTA C460**, to make clear at a glance that it is a community project.

> **Unofficial.** This project is not affiliated with, endorsed by or supported by Arista Networks. It changes files on the AP and uses interfaces the vendor does not document for this purpose. Use it at your own risk, and keep console access available while you experiment.

## Features

- **Overview:** health, clients, radios, uplink, temperature, memory, flash usage.
- **Wireless networks (read/write):** create, edit and delete SSIDs: name, security (WPA3 Personal, WPA2 Personal, Enhanced Open, Open), password, bands (2.4/5/6 GHz), VLAN tag or untagged, client isolation, hidden, enabled.
- **Advanced wireless settings (read/write):** per-SSID 802.11k radio measurements and BSS-load advertising, with firmware-default choices and actual per-band driver readback.
- **Switch discovery (read/write):** live LLDP neighbours and ports, advertisement interval and hold multiplier. Explicit timing settings are stored locally and restored if the daemon resets them.
- **Radios (read/write):** channel (regulatory list, DFS marked), channel width, transmit power, automatic channel/power, enable/disable. It shows the effective EIRP, channel utilisation and noise floor.
- **Clients:** signal, SNR, rates, traffic, IPv4/IPv6 and hostname (including static IPv4 learned through ARP), plus live association details and a confirmed reconnect action. Reconnect briefly disconnects the station; it does not ban it.
- **RF scan:** neighbouring access points with channel occupancy.
- **Management network (read/write):** static IPv4 or DHCP client, subnet mask, gateway, up to three DNS servers, DNS search domain, and native/untagged or tagged management VLAN. Saved changes apply after an AP restart; the UI shows the destination address.
- **Time synchronisation (read/write):** primary and secondary NTP servers, service and clock-sync status. Saves use the native encrypted configuration, with local desired settings restored when the WebUI service starts.
- **Diagnostics:** AP-side ping, DNS lookup, route tracing and TCP-port connectivity; VLAN/bridge paths, management routes and learned neighbours; searchable wireless connection, channel and radar events with text export; and live per-BSSID operating state. Tests have fixed time/output limits. Client details can prefill a connection test.
- **System:** device/VLAN display names, SSH enable/disable, timed LED location, restart with firmware boot-trust checks, hardware/power/clock/LLDP information, Ethernet ports, and administrator username and password.
- **Backup and restore:** download SSIDs, radios, management network, names, time servers and LLDP timing as a JSON file and apply it to the same or another C-460, choosing which parts to apply. Wi-Fi passwords are left out unless you set a passphrase; then they are encrypted with it (scrypt + AES-256-GCM). Management addresses are off by default when copying, so a second AP does not take over the first one's IP.
- **SNMP monitoring (read-only):** optional SNMP v1/v2c agent on UDP 161 with MIB-II system group, ifTable and ifXTable (64-bit counters) for both Ethernet sockets. ifIndex 1/2 always means ETH 1/ETH 2. Community, location and contact are set under Network; writes are always refused.
- **HTTPS:** served on port 443 with a self-signed certificate generated on first start, next to plain HTTP on port 80.
- **Live updates:** shared AP sampling configurable from 1–60 seconds (default 5). Browsers follow that cadence, pause in background tabs, share pending requests and back off during an outage. Slow AP reads can extend the interval; detailed hardware information remains a separate, slower sample.
- A small pirate hat spins continuously while the page starts and waits for AP data, including after a restart. Reduced-motion preferences are respected.
- Light, dark and system themes; works on phones.

## How it works

```
browser ──HTTP :80──▶ c460-webui (Go, on the AP) ──gNMI/TLS 127.0.0.1:8080──▶ AP OpenConfig agent ──▶ radios
```

The backend is a single static ARM64 Go binary with the React UI embedded (about 12 MB). Wireless and SSH settings use the AP's OpenConfig (gNMI) agent. Management addresses, DNS and management VLAN are staged in the firmware's native `ifcfg-br0[.<VLAN>]` and discovery configuration files, preserving IPv6 and other discovery fields; the native management CLI reboots automatically, so it is deliberately not used for saving. Writes use the vendor's interface lock and roll back on failure. The AP consumes the saved settings at the next explicit restart. A boot-ID marker keeps the pending-restart notice accurate across web-service restarts. LED location and reboot use the native vendor CLI. The agent's TLS certificate is pinned from `/opt/openconfig/cert/agent.crt`. Every OpenConfig AP-level write also re-sends the API user; this firmware otherwise resets API authentication on such writes.

Tested on a C-460 with firmware **18.2.0-32**. See [native feature findings and verification](docs/native-features.md) for the additional root-access adapters and their limits.

## Requirements per access point

1. **Root SSH with a key.** The deploy script installs over SSH as `root`.
2. **OpenConfig mode enabled** with an API user. You need that user's name and password.
3. The AP needs a management IP that your computer can reach.

Steps 1 and 2 are device-specific bootstrap work and are not automated by this repository.

## Install

Build requirements: Go ≥ 1.26, Node.js ≥ 20, `make`.

```bash
cp deploy/gnmi-credentials.example.json secrets/ap1.secret.json   # gitignored; fill in
deploy/deploy.sh 192.168.1.40 --gnmi-credentials secrets/ap1.secret.json --site-name "Stage left"
```

On the first install the script asks for the web UI administrator login: the username defaults to `config`, and you choose the password (at least 6 characters; no default password is shipped). It then:

- installs `/opt/c460-webui/c460-webui`, `config.json` (mode 0600) and `auth.json` (bcrypt hash);
- installs the procd script as `/opt/c460-webui/init/c460-webui` and links it from `/opt/init.d/c460-webui` and `/etc/rc.d/S0900c460-webui` (symlinks only, see below);
- starts the service, checks `http://<ap>/` and runs the boot-trust pre-check.

The same command updates an existing install; the configuration and password are kept. Other options:

```bash
deploy/deploy.sh <ap> --set-password            # reset the UI login (e.g. forgotten password)
deploy/deploy.sh <ap> --username admin          # use a different login name
deploy/deploy.sh <ap> --vlan-names vlans.json   # {"10": "Office", "20": "Guests"} shown next to VLAN ids
deploy/deploy.sh <ap> --uninstall
```

Firmware upgrades or factory resets probably remove the installation; deploy again afterwards.

### Firmware boot-time trust check (read this before changing the AP)

At every boot the C-460 firmware (`/etc/rc.d/fs_init`) checks its writable layer. If it finds anything it does not trust, it **deletes the entire writable layer** (`/overlay/upper`: your OpenConfig mode, API users, SSH keys, this UI…) and boots the other firmware partition. It fails the check when:

- any **regular file** is in a protected directory: `/usr`, `/bin`, `/etc`, `/lib`, `/lib64`, `/sbin`, `/opt/init.d`, `/opt/lib`, `/opt/keys`, `/opt/scripts`, `/opt/sensor/scripts`, `/opt/dhclient`, `/opt/udhcpc` (symlinks are fine; exempt: `/etc/resolv.conf`, `/etc/profile`, `/usr/local/etc/`, `/lib/firmware/`). This includes Python bytecode caches: run any Python on the AP as `python3 -B`;
- `/opt/passwd`, `/opt/group`, `/opt/shells`, `/opt/sensor/sensor-shell`, `/opt/sensor/sensor.md5` or `/etc/shadow` differ from the image (they are measured into TPM PCR 7);
- a vendor file listed in `/opt/sensor/sensor.md5` was modified.

Custom executable files live under `/opt/c460-webui/`, with symlinks in the protected service directories. Settings use the native writable network/discovery files and encrypted sensor configuration. Before rebooting after **any** manual change on the AP, run:

```bash
deploy/deploy.sh <ap> --check      # runs deploy/overlay-check.sh on the AP, read-only
```

### Configuration file

`/opt/c460-webui/config.json` on the AP:

| Key | Default | Meaning |
|---|---|---|
| `listen` | `:80` | HTTP listen address |
| `httpsListen` | `:443` | HTTPS listen address; `off` disables HTTPS |
| `tlsDir` | `tls/` next to the config | Self-signed certificate and key (generated on first start) |
| `pollSeconds` | `5` | Shared AP sampling interval, 1–60 seconds; editable in System → Live updates |
| `authFile` | `/opt/c460-webui/auth.json` | Username and bcrypt password hash |
| `hostname` | eth0 MAC with dashes | OpenConfig access-point key |
| `siteName` | — | Label shown in the UI |
| `vlanNames` | — | Optional VLAN id → name map |
| `gnmi.username` / `gnmi.password` | — | OpenConfig API user (required) |
| `gnmi.address` | `127.0.0.1:8080` | Agent address |
| `snmp` | off | `enabled`, `community`, `location`, `contact`; editable under Network → Monitoring. `listen` (default `:161`) is file-only |

## Security notes

- The UI is served over HTTP and over HTTPS with a self-signed certificate; use it on a trusted management network. SNMP v1/v2c sends the community in clear text, so only enable it on the management network. Sessions use an HttpOnly, SameSite=Strict cookie. Login failures are rate-limited, and changes require a JSON request.
- Wi-Fi passwords are never sent to the browser.
- `config.json` contains the OpenConfig API password; keep it at mode 0600 and never commit it (`*.secret.json` and `secrets/` are gitignored).

## Development

```bash
make check                                     # go vet + TypeScript
ssh -L 18099:127.0.0.1:80 root@<ap>            # tunnel to an installed backend
make dev                                       # Vite on http://localhost:5175, /api proxied to the tunnel
make build                                     # web + ARM64 binary in build/
```

Project layout: `*.go` contains the backend (`gnmi.go` agent client, `state.go` state model, `api.go` HTTP API, `auth.go` sessions, `system.go` device info). `web/` contains the React + Tailwind UI, and `deploy/` the install script and procd service.

## Known limitations

- Management configuration currently covers IPv4. DHCP mode configures the AP as a DHCP client, not a DHCP server. It does not change router/switch settings or wireless-client DHCP scopes. Tagged management needs a matching switch trunk; a new address must be reachable from the administrator's network.
- Ethernet uplink selection and IPv6 management are not editable yet. Hardware, clock-sync and Ethernet status are shown where available.
- The AP's OpenConfig converter rejects some modelled fields (for example WPA2/WPA3 transition mode, DHCP-required, some 802.11r/v timers), so the UI does not offer them.
- Client detail fields are only shown when the firmware populates them.

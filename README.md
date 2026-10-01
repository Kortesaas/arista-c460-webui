# arista-c460-webui

A local, self-hosted web interface for the **Arista C-460** Wi-Fi 7 access point. It runs on the access point itself and lets you manage it from a browser at `http://<ap-ip>/`, like the built-in web UI of a typical standalone access point. No controller and no cloud service are needed.

The UI carries a parody brand, **ARRR-ISTA C460**, to make clear at a glance that it is a community project.

> **Unofficial.** This project is not affiliated with, endorsed by or supported by Arista Networks. It changes files on the AP and uses interfaces the vendor does not document for this purpose. Use it at your own risk, and keep console access available while you experiment.

## Features

- **Overview:** health, clients, radios, uplink, temperature, memory, flash usage.
- **Wireless networks (read/write):** create, edit and delete SSIDs: name, security (WPA3 Personal, WPA2 Personal, Enhanced Open, Open), password, bands (2.4/5/6 GHz), VLAN tag or untagged, client isolation, hidden, enabled.
- **Radios (read/write):** channel (regulatory list, DFS marked), channel width, transmit power, automatic channel/power, enable/disable. It shows the effective EIRP, channel utilisation and noise floor.
- **Clients:** signal, SNR, rates, traffic, IP/hostname where the AP reports them.
- **RF scan:** neighbouring access points with channel occupancy.
- **System:** device and firmware information, Ethernet ports, administrator username and password.
- Light, dark and system themes; works on phones.

## How it works

```
browser ──HTTP :80──▶ c460-webui (Go, on the AP) ──gNMI/TLS 127.0.0.1:8080──▶ AP OpenConfig agent ──▶ radios
```

The backend is a single static ARM64 Go binary with the React UI embedded (about 12 MB). It reads and writes configuration through the AP's own OpenConfig (gNMI) agent, so changes are validated and stored by the firmware itself and survive reboots. The agent's TLS certificate is pinned from `/opt/openconfig/cert/agent.crt`. Every AP-level write also re-sends the API user; this firmware otherwise resets API authentication on such writes.

Tested on a C-460 with firmware **18.2.0-32**.

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
- installs the procd script as `/opt/c460-webui/c460-webui.init` and links it from `/opt/init.d/c460-webui` and `/etc/rc.d/S0900c460-webui` (symlinks only, see below);
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

This project only writes under `/opt/c460-webui/` and creates symlinks. Before rebooting after **any** manual change on the AP, run:

```bash
deploy/deploy.sh <ap> --check      # runs deploy/overlay-check.sh on the AP, read-only
```

### Configuration file

`/opt/c460-webui/config.json` on the AP:

| Key | Default | Meaning |
|---|---|---|
| `listen` | `:80` | HTTP listen address |
| `pollSeconds` | `5` | How often the AP state is read |
| `authFile` | `/opt/c460-webui/auth.json` | Username and bcrypt password hash |
| `hostname` | eth0 MAC with dashes | OpenConfig access-point key |
| `siteName` | — | Label shown in the UI |
| `vlanNames` | — | Optional VLAN id → name map |
| `gnmi.username` / `gnmi.password` | — | OpenConfig API user (required) |
| `gnmi.address` | `127.0.0.1:8080` | Agent address |

## Security notes

- The UI is plain HTTP; use it on a trusted management network. Sessions use an HttpOnly, SameSite=Strict cookie. Login failures are rate-limited, and changes require a JSON request.
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

- Management IP, gateway, DNS and uplink settings are not editable yet. The firmware handles those outside OpenConfig.
- The AP's OpenConfig converter rejects some modelled fields (for example WPA2/WPA3 transition mode, DHCP-required, some 802.11r/v timers), so the UI does not offer them.
- Client detail fields are only shown when the firmware populates them.

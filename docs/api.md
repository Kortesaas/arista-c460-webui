# Local AP API — version 1

Use this API from a Raspberry Pi, laptop, server, or automation on a network
that can reach the AP's management address. No controller or cloud is needed.
The API runs alongside the WebUI on the same HTTP and HTTPS ports.

Base URL: https://192.168.99.40/api/v1
Specification: GET /api/v1/openapi.json (OpenAPI 3.0.3)
Guide: GET /api/v1/docs

## Getting access

In System → API access, create a named token for each integration. Save the
returned secret: it is shown only once. Only its SHA-256 hash is stored on the
AP, in auth.json.tokens.json beside the login file, mode 0600. Tokens survive
service restarts and AP power cycles. Revoke a token in System to invalidate it
immediately; rebooting does not revoke tokens. A firmware reset removes them
along with this installation. Changing the browser password does not revoke
tokens: revoke integrations separately when needed.

Every request uses:
  Authorization: Bearer YOUR_TOKEN
Writes additionally use:
  Content-Type: application/json

Token permissions are additive:
  monitor   Read status, configuration without secrets, history, events,
            capabilities; run bounded ping, DNS and TCP diagnostics; download
            support bundles and view packet-capture job metadata.
  configure Create/edit/delete wireless networks, VLAN assignments, radio
            settings, Wi-Fi 7, advanced features, schedules, management IP,
            gateway/DNS, time, LLDP, labels, polling, SNMP, metrics, bandwidth and QoS.
  control   Reboot the AP, enable/disable SSH, locate LEDs, reconnect clients.
            Start, stop and delete packet captures also require secrets.
  secrets   Retrieve Wi-Fi join credentials, SNMP communities, metrics tokens,
            and create backups (passwords are encrypted with your passphrase).
            Download PCAP files, which can contain private packet payloads.

Every token includes monitor. Restore requires configure + control + secrets.
Tokens cannot change browser accounts or create/revoke other access tokens.
Only an administrator browser session can manage tokens.
Maximum: 32 named tokens. Expiry: 0 (no expiry) or 1–3650 days.
Use different tokens for monitoring and configuration integrations.

The AP's HTTPS certificate is local. Copy /opt/c460-webui/tls/cert.pem over
trusted SSH and use it as the client's CA file; use the hostname/IP listed in
that certificate. A standard public CA certificate can also be installed.
HTTP is available on trusted isolated LANs, but carries bearer tokens in clear
text. This API does not need an internet connection or cross-origin browser
access. No CORS wildcard is enabled.

## Quick start

Set C460_URL to the AP origin (for example https://192.168.99.40), C460_TOKEN to
your token, and C460_CA to your downloaded AP certificate when using local TLS.

curl --cacert "$C460_CA" -H "Authorization: Bearer $C460_TOKEN" \
  "$C460_URL/api/v1/device"

curl --cacert "$C460_CA" -H "Authorization: Bearer $C460_TOKEN" \
  "$C460_URL/api/v1/clients?ssid=FOH-MGMT&band=6"

Python client and CLI: examples/c460_client.py in the repository.
It uses only Python's standard library and runs on Raspberry Pi OS.

  python3 examples/c460_client.py GET device
  python3 examples/c460_client.py GET state
  python3 examples/c460_client.py GET capabilities
  printf '{"tool":"ping","target":"192.168.99.1"}' | \
    python3 examples/c460_client.py POST diagnostics --body -

## Monitoring endpoints (monitor)

GET /state                     Full WebUI snapshot: device, radios, SSIDs,
                               clients, neighbors, Ethernet, management,
                               hardware, health, schedules and time zone.
GET /device                    CPU %, memory, storage, temperature, firmware,
                               uptime, management IP and device identity.
GET /radios                    Current radio settings, utilization, channel,
                               power, regulatory limits and Wi-Fi 7 readback.
GET /radios/{id}                One radio (use IDs returned by /radios).
GET /ssids                     Configured networks, bands, VLAN, client count
                               and traffic. Passwords are never in these reads.
GET /ssids/{name}               One network.
GET /ssids/{name}/policy        Saved client access policy and independent
                               driver mode, MAC list and limit for each band.
GET /ssids/{name}/traffic       Desired limits/QoS and verified kernel queues.
GET /clients                   Associated clients; optional ssid and band
                               query filters. Rates, RSSI, SNR, IP, byte counters.
GET /clients/{mac}              One associated client; 404 after disconnection.
GET /clients/{mac}/details      Native hostapd station readback.
GET /clients/{mac}/history      Recent in-memory per-client samples.
GET /neighbors                 Nearby Wi-Fi observations.
GET /interfaces                Ethernet links, speeds and counters.
GET /health                    Derived health findings, severity and detail.
GET /management                Saved management IP, mask, gateway, DNS,
                               native/tagged VLAN, pending-reboot status.
GET /settings                  Display names, VLAN names, time zone, polling.
GET /network                   Native bridges, VLANs, routes and neighbors.
GET /wireless-status           Native hostapd interface readback.
GET /lldp                      LLDP timing and discovered wired neighbors.
GET /time                      Time servers, service and sync status.
GET /snmp                      SNMP service/settings (secret redacted).
GET /metrics                   Prometheus settings (token redacted).
GET /history?hours=1            In-memory resource/radio/client history.
GET /events                    Recent cached native Wi-Fi events.
GET /changes                   Configuration audit log (latest 300 changes).
GET /trust                     Firmware boot-time writable-layer trust check.
GET /ssids/{name}/features      Advanced settings and native readback.
GET /radios/{id}/features       Advanced settings and native readback.
GET /capabilities              Versions, permissions, supported settings,
                               ranges, widths, bands and links to the reference.
GET /openapi.json               Machine-readable API paths and JSON schemas.
GET /docs                      This guide, served locally as HTML.
POST /diagnostics              {tool:"ping"|"dns"|"trace"|"tcp",target,port?}.
                               trace runs bounded traceroute (8 hops).
                               TCP needs port 1–65535. Timeout is 15 seconds.
GET /captures                  Capture availability, current interfaces, jobs
                               and bounds. Metadata only; monitor permission.
GET /captures/{id}             One job; 404 if missing/expired.
POST /support-bundle           {includeClients:false,includeEvents:true}.
                               Returns a ZIP download; monitor permission.

Collection resources /device, /radios, /ssids, /clients, /neighbors,
/interfaces, /health, /management and /settings return:
  {"generatedAt":"RFC3339 timestamp","pollSeconds":5,"error":"","data":...}
Single-resource reads return the object directly. /state retains its full
existing shape for WebUI compatibility. Empty arrays may be null where the
firmware has no observations. Treat an absent/null numeric reading as unknown,
not zero. Byte counters are cumulative; rates are Mb/s, utilization and CPU %,
RSSI/SNR dBm/dB. See schemas in the specification for exact field names.

Fast reads reuse cached samples and do not start new hardware polls. Check
generatedAt / X-Sampled-At and pollSeconds / X-Poll-Seconds to distinguish new
samples from repeated data. The normal poll cadence is 5 seconds. Read /state
or /device every 5 seconds for monitoring; a 1-second request does not make a
5-second sample fresher. Driver diagnostics and trust checks are more expensive
and should be requested on demand. History starts empty after service restart.

## Bandwidth limits and QoS

On tested C-460 firmware 18.2.0-32, use Wireless networks → Bandwidth and QoS
or Clients → View → Bandwidth limits. API reads require monitor; writes
require configure. URL-encode SSID names and MAC addresses.

GET /ssids/{name}/traffic
PUT /ssids/{name}/traffic
PUT /ssids/{name}/traffic/clients/{mac}
DELETE /ssids/{name}/traffic/clients/{mac}

Complete network PUT example (4 Mbps shared download, 1 Mbps shared upload):

```json
{
  "bandwidth": {"uploadKbps": 1000, "downloadKbps": 4000},
  "perClient": {"uploadKbps": null, "downloadKbps": null},
  "qos": null,
  "clients": {}
}
```

Rates use decimal Kbps from the wireless client's perspective. Each direction
accepts null for unlimited, or an integer from 32 to 1,000,000. The SSID cap is
shared across its bands and devices. perClient gives each associated device a
separate cap; new associations receive it on reconciliation (normally within
five seconds after AP telemetry sees the client). The clients map accepts up
to 128 unicast MAC overrides, including devices currently offline. An override
replaces both default directions; null/null explicitly exempts that device
from the per-client default. The shared SSID cap still applies. DELETE removes
an override and resumes inheritance. Device PUT requires both uploadKbps and
downloadKbps; network PUT requires all four top-level fields and both rates in
every limits object. Unknown fields and incomplete requests are rejected on
both API routes, including the legacy /api alias.

Optional qos is null to restore native firmware settings, or a complete object:

```json
{"priority":"voice","mode":"fixed","mapping":"dscp","markDSCP":true,"mark8021p":false}
```

priority: voice, video, best-effort or background. mode: ceiling (retain lower
packet priorities) or fixed (assign the network priority). mapping selects
downstream dscp, 8021p or legacy tos. markDSCP and mark8021p enable upstream
marking. Dedicated audio/control networks can use fixed voice; mixed traffic
can use a voice ceiling and DSCP from the sending devices. These are WMM
traffic classes, not automatic application recognition or reserved airtime.
The driver has no exact operating QoS getter: qosStatus explicitly reports
configured-no-driver-readback, pending or error. Configuration acceptance does
not establish relative performance under competing audio/video loads.

The response contains settings, supported, managed, applied, pending,
qosStatus, queues and optional error. applied means bandwidth rates, IPv4/IPv6
redirection and complete MAC filters were independently verified in the kernel;
for custom QoS it also requires successful setters on current interfaces.
Queue entries report direction, optional device MAC, limitKbps, bytes, packets,
drops and overlimits. Overlimits are scheduling deferrals, not packet drops.
Counters reset when queues are rebuilt. GET shows saved policy and operating
queues separately; it does not change configuration.

Desired settings are atomically saved in traffic-policies.json (0600). A
manager restart, recreated interfaces and new clients trigger reconciliation.
Renames carry settings; deletion clears the old profile before its mapping is
removed. Failed apply, readback or persistence attempts roll back runtime
settings and leave desired settings unchanged. Inactive networks retain
pending settings. Wireless backups include traffic policies; old backups
without them preserve retained policies. The native device has eight shared
SSID queue pairs; exhausted capacity is rejected. Other firmware is refused.

Speed caps do not emulate weak RF, injected latency, jitter or loss. Payload
throughput is lower than the configured link rate because of packet overhead,
TCP behavior and measurement timing. Reapplying limits briefly rebuilds that
network's queues; it does not restart its radios.

## Packet capture and support downloads

POST /captures                  Start a job; returns 202 and its ID.
POST /captures/{id}/stop        {}. Idempotently request cancellation.
DELETE /captures/{id}          Delete a finished job and its file. Stop first.
GET /captures/{id}/download    PCAP file; 409 while running or after failure.

Start, stop and delete require an administrator session or a token with
control + secrets. PCAP downloads require an administrator session or secrets.
Viewers/monitor tokens can inspect metadata but cannot operate or download
captures. All routes also exist under /api/v1 and require authentication.

Example complete capture request:

```json
{"interface":"eth0","protocol":"udp","host":"192.0.2.10","port":5353,
 "seconds":10,"maxBytes":1048576,"snapLength":128}
```

Select an enabled interface returned by GET /captures. "any" captures across
interfaces; bridges and wireless data interfaces can see duplicate traffic.
No promiscuous or monitor mode is enabled; hardware offload can hide traffic.
This is data capture, not raw 802.11/radiotap capture. Filters are structured:
protocol is all/arp/icmp/tcp/udp; host is a numeric IPv4/IPv6 address; port 0
means any port and cannot be combined with ARP/ICMP. Raw BPF and command-line
options are not accepted. There is no hostname lookup during capture.

Defaults are 10 seconds, 1 MiB and 128 bytes/packet. Duration is 1–120 seconds,
size 65536–8388608 bytes and snap length 64–4096 bytes. One capture runs at a
time (409 on overlap). Capture stops at the first time/size limit and writes
only complete PCAP records. Jobs report running/completed/stopped/failed,
reason, packet/file counts, timestamps, expiry and download availability.
Interrupted captures retain their complete packets for download. Failed files
are discarded. A valid header-only file means zero packets matched.

Up to three jobs are retained in private temporary storage for ten minutes
after completion. A new job evicts the oldest completed job when needed.
Manager/AP restart clears captures; Linux also signals tcpdump if the manager
dies. Download before expiry and open the PCAP in Wireshark. Packet payloads
can contain credentials even when only short packets are retained.

POST /support-bundle returns application/zip with manifest.json, state.json,
network.json, wireless-status.json, available management/hardware status and
optional parsed events. includeClients defaults false; includeEvents defaults
false, and the WebUI explicitly selects events by default. Client details and
network-neighbour addresses follow includeClients; event client MACs are
removed when false. Details are bounded to 512 clients/neighbours and 150
events. Snapshots record collection times and missing/stale-data warnings.
Only one bundle is built at once, within a 15-second collection deadline and
a 2 MiB uncompressed/archive limit. Missing optional reads appear as warnings.

Bundles omit passwords, key/token/community fields, accounts/client usernames,
raw configuration/logs/crash dumps, nearby Wi-Fi observations and packet files.
They retain network names, AP identifiers and management addresses. Review
these before sharing. A support bundle is diagnostic data; use /backup for a
restorable configuration. Both download routes return binary content on
success and the usual JSON error object on failure; never parse them as JSON.

## Configuration endpoints (configure)

Coverage follows the controls currently implemented in the WebUI. Enterprise
RADIUS/802.1X,
IPv6 management, MLO, firmware updates and preferred uplink selection are not
currently exposed. Some need additional implementation and verification;
others encounter firmware restrictions. Webhooks/push subscriptions and
persistent history are also not implemented. See README.md for coverage and
docs/native-features.md for the device verification record.

POST /ssids                    Create a network with the complete SSID body.
PUT /ssids/{name}              Replace ordinary settings (body name permits
                               rename). Empty password preserves current key.
DELETE /ssids/{name}           Delete a network.
PUT /ssids/{name}/policy       Replace client access settings. See below.
PUT /ssids/{name}/features     Merge specified advanced keys; null restores a
                               firmware default; omitted keys stay unchanged.
PUT /ssids/{name}/schedule     {enabled,windows:[{days:[0..6],start:"HH:MM",
                               end:"HH:MM"}]}; Sunday=0, overnight supported.
PUT /radios/{id}               Complete ordinary radio settings body.
PUT /radios/{id}/features      Merge advanced radio keys; null resets default.
PUT /radios/{id}/wifi7         {enabled,width:160|320}; supported 6 GHz radio.
POST /batch                   {changes:[{kind:"ssid-create"|"ssid-update"|
                               "ssid-delete"|"radio",name?,id?,ssid?,radio?}]};
                               maximum 32; coalesces ordinary Wi-Fi changes.
PUT /management               {mode:"static"|"dhcp",ipv4,netmask,gateway,
                               dns:[...],dnsSearch,commVlan?}. "untagged" for
                               native management VLAN; omit to preserve it.
PUT /settings                 {siteName,vlanNames:{"10":"Control",...}}.
PUT /refresh                  {seconds}; changes sampling cadence globally.
PUT /time                     {primary,secondary} (NTP names/addresses).
PUT /timezone                 {timeZone:"Europe/Berlin"} (IANA zone).
PUT /lldp                     {interval,hold} (seconds/multiplier).
PUT /snmp                     {enabled,community,location,contact,listen?}.
PUT /metrics                  {enabled,newToken}; regenerates exporter token
                               when requested. secrets needed to read token.

SSID body (send all ordinary settings, even unchanged values):
  {"name":"Test","enabled":true,"hidden":false,"opmode":"WPA3_SAE",
   "password":"YOUR_WIFI_PASSWORD","bands":["2.4","5","6"],
   "vlan":99,"isolation":false}

vlan:null means untagged; vlan:99 means tagged VLAN 99, not native management.
Keep the AP uplink's existing trunk/native VLAN arrangement in mind. The API
does not change a switch or router and does not provide client DHCP.
Security modes: WPA3_SAE, WPA2_WPA3_PERSONAL, WPA2_PERSONAL, ENHANCED_OPEN, OPEN.
Use /capabilities.securityModes for the authoritative firmware-facing names.
6 GHz requires compatible security; the server validates unsupported mixes.

Radio body (send all ordinary settings):
  {"enabled":true,"channel":5,"width":160,"power":23,"dca":false,"dtp":false}

Use regulatory allowedChannels/maxTxPower from /radios. Ordinary widths:
2.4 GHz 20/40; 5 GHz 20/40/80/160; 6 GHz up to 160. Use /wifi7 for 320;
OpenConfig's ordinary width field cannot encode 320. Native Wi-Fi 7 readback
reports requested and actual operating mode/width separately.

Advanced keys and numeric ranges come from /capabilities and the specification.
Current SSID keys: rrm, load, bssTransition, fastRoaming, okc, bandSteering,
multicastFilter, broadcastFilter, advertiseName, plus supported native features
listed by GET /ssids/{name}/features. Radio keys: dlOfdma, ulOfdma, dlMuMimo,
ulMuMimo, bssColoring, spatialReuse, dtpMin, dtpMax.

Client access is supported on tested C-460 firmware 18.2.0-32. Send the complete
body, for example:

```json
{"macFilter":{"mode":"deny","addresses":["02:00:00:46:00:01"]},"maxClients":7}
```

Modes are `off`, `allow`, and `deny`. An allow list needs at least one address.
Up to 128 unique unicast Wi-Fi MAC addresses are accepted and returned in
canonical sorted form. Filtering off retains the saved list but clears the
operating driver list. Phones can use a private MAC address for each network;
use that network's address. MAC filtering supplements the Wi-Fi password.
`maxClients` is 1–127 **per band/BSS**, or `null` for the firmware default of
127. The response separates `settings` from `interfaces` operating readback;
a null getter means unknown. Inactive networks have no operating readback.
The policy follows SSID renames and is removed on deletion. Desired settings
are saved under `/opt/c460-webui/`, restored after ordinary wireless changes,
and included in wireless backups. Failed native/readback/persistence updates
attempt to restore the previous owned fields and return an error.

Wireless changes may interrupt Wi-Fi briefly; batch related ordinary changes.
Management changes are staged for an explicit reboot. Check rebootRequired
in the result before POST /reboot, then reconnect at the new management IP.
Accepted settings are not proof the firmware has finished applying them:
check subsequent samples and native feature/Wi-Fi 7 readback. Native operations
and multi-section restore are not globally atomic; inspect errors/results.

## Device actions (control)

POST /reboot                  {}. Includes firmware trust pre-check. This
                               disconnects clients and the management service.
PUT /ssh                      {enabled:true|false}.
POST /locate                  {minutes:1} (bounded LED identification).
DELETE /locate                Stop identification LEDs.
POST /clients/{mac}/reconnect {}. Disconnect client so it reconnects.

## Secrets and backups

GET /ssids/{name}/join        Wi-Fi password and join payload (secrets).
POST /backup                 {passphrase:"YOUR_BACKUP_PASSPHRASE"}; secrets.
                              Passwords included only encrypted when supplied;
                              use empty passphrase to omit passwords.
                              Includes client access policies, including off
                              defaults. Older backups without ssidPolicies
                              preserve policies on retained networks.
POST /restore                {backup,passphrase,sections:{wireless,radios,
                              management,labels,time,lldp},removeOthers,
                              management?}; configure + control + secrets.
                              Use a backup returned by this AP's API.

## Token management and browser sessions

These operations require an administrator session cookie, not an API token:
GET /tokens                  List metadata; never returns secrets or hashes.
POST /tokens                 {name,scopes:["monitor",...],expiresDays:30}.
                              201 {token:"c460_...",access:{id,name,scopes,
                              createdAt,expiresAt}}; secret is shown only once.
DELETE /tokens/{id}           Revoke immediately. Cookie clients include
                              X-Requested-With: c460-webui for DELETE.
POST /password               {current,username,next}; browser login change.
PUT /viewer                  {username,password}; read-only browser account.
DELETE /viewer               Remove read-only browser account.

POST /login                  {username,password}; sets c460_session cookie.
GET /session                 Authentication state and role.
POST /logout                 {}; invalidates browser session.
Sessions expire after 12 hours and service restart. Persistent tokens are the
preferred mechanism for computers. Existing /api paths remain compatible;
new clients should use /api/v1. No additional port or service is needed.

## Errors, retries and audit

Errors use JSON {"error":"explanation"}. Typical HTTP codes:
400 invalid input; 401 missing/invalid/expired/revoked credentials;
403 permission denied; 404 absent resource; 415 non-JSON write;
429 login rate limit; 500 native operation failed; 503 not ready.
A successful HTTP response can still contain device-level warning/error fields,
or success:false for a diagnostic. Inspect those fields and sample timestamps.
Writes accept JSON up to 64 KiB. Names and MACs must be URL-encoded as path
segments. Unknown input fields are rejected. PUT SSID/radio is a complete
settings request, not a partial patch. Do not send a partial ordinary body.

Automatic retries should be limited to reads. Do not blindly retry POST, batch,
restore, or reboot after a lost connection: a request may have been applied.
Read back the configuration first. Writes are serialized where native settings
require it. Successful/failed authorized configuration changes are audited with
integration name and source IP; passwords/tokens are not written to the log.

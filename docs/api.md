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
            capabilities; run bounded ping, DNS and TCP diagnostics.
  configure Create/edit/delete wireless networks, VLAN assignments, radio
            settings, Wi-Fi 7, advanced features, schedules, management IP,
            gateway/DNS, time, LLDP, labels, polling, SNMP and metrics.
  control   Reboot the AP, enable/disable SSH, locate LEDs, reconnect clients.
  secrets   Retrieve Wi-Fi join credentials, SNMP communities, metrics tokens,
            and create backups (passwords are encrypted with your passphrase).

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
POST /diagnostics              {tool:"ping"|"dns"|"tcp",target,port?}.
                               TCP needs port 1–65535. Timeout is 15 seconds.

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

## Configuration endpoints (configure)

POST /ssids                    Create a network with the complete SSID body.
PUT /ssids/{name}              Replace ordinary settings (body name permits
                               rename). Empty password preserves current key.
DELETE /ssids/{name}           Delete a network.
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

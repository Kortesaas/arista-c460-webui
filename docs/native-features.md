# Native features on C-460 firmware 18.2.0-32

Root access provides additional interfaces beyond this firmware's OpenConfig converter. This is a record of observed behavior, not a claim that every installed feature works locally.

| Feature | Device interface | WebUI integration | Verification |
|---|---|---|---|
| Primary / secondary NTP server | Native `sensor.conf` keys `ntpserver` / `secntpserver`, TPM-backed encrypted copy, vendor `ntpd.init` | System → Time synchronisation | Temporary secondary server saved and read back; running ntpd arguments checked; survived a WebUI service restart; original servers restored. Encrypted copy was decrypted privately and compared with plaintext. |
| Ping and route tracing | BusyBox, separate argument array | Diagnostics → Network tests | Loopback ping and traceroute succeeded on the AP. Targets with shell syntax rejected. |
| DNS lookup | BusyBox nslookup, management DNS | Diagnostics → Network tests | Actual AP result displayed correctly. DNS lookup failed in the router-free test environment; successful upstream resolution remains dependent on a reachable DNS server. |
| Live BSS status | Hostapd Unix datagram control sockets | Diagnostics → Live wireless interfaces | All 15 interfaces returned ENABLED, BSSID, SSID, frequency, beacon and DTIM values. Actual DTIM was 2, while a native profile file declared 1: operating-state reads matter. |
| Client association details | Hostapd `STA <mac>` | Clients → View | Socket protocol and allowlisted properties tested. AP rejected absent clients. A test phone on the management SSID later returned live association flags, interface, traffic counters and connected time. |
| Reconnect client | Hostapd `DEAUTHENTICATE <mac> reason=2` | Clients → View → Reconnect client | Backend tested against a local protocol fixture for success, failure and absent stations; confirmation and success UI tested with a simulated client. Later verified on the real test phone: the AP accepted the action, the station rejoined with AUTHORISED flags within one second, its connected-time counter reset, and its static address remained pingable. |
| Static client IPv4 discovery | Complete entries in `/proc/net/arp` | Clients table and details, with address source | The AP pinged a manually configured test phone with 0% loss; its ARP entry supplied the address missing from OpenConfig. Incomplete and ambiguous entries are ignored. |
| 802.11k radio measurements | OpenConfig SSID `dot11k`; native `cfg80211tool get_rrm` | Wireless → Advanced | Hidden test network toggled enabled/disabled; actual driver values matched. Subsequently verified enabled/disabled on an existing SSID across 2.4, 5 and 6 GHz, then restored its defaults. Missing configuration is shown as firmware default rather than false. |
| BSS-load advertising | OpenConfig SSID `qbss-load`; native `cfg80211tool get_qbssload` | Wireless → Advanced | Hidden test network toggled enabled/disabled; deleting the explicit leaf restored the firmware default of enabled. Enabled/disabled/default readback also matched on all three bands of an existing SSID. |
| LLDP timing and neighbours | Native `lldpcli` configuration and detailed JSON neighbours | System → Switch discovery | Interval 31 and hold 5 read back; an outgoing LLDP packet carried TTL 155. Saved timing survived a WebUI restart and was reapplied after a native timing reset. Original timing restored. No real neighbour was advertising in the test environment; neighbour parsing uses fixtures for object/list forms. |
| TCP-port connectivity | Go TCP dial with validated target and port, 5-second connection deadline | Diagnostics → Network tests | Real AP connection to local HTTP port succeeded; a closed port returned a failure. Automated loopback socket tests cover success and refusal. |
| 6 GHz Wi-Fi 7 / 320 MHz | Native radio-v2 profile, vendor trigger/config manager, driver mode and kernel width | Radios → 6 GHz → Wi-Fi 7 | Actual EHT160 and EHT320 verified on the AP; disabling returned to HE160. Creating a 6 GHz-only SSID through OpenConfig restored EHT320 automatically. Full AP reboot retained root SSH, WebUI, native management bridge mapping, all 16 BSSs and EHT320. Automated tests cover rollback and persistence. |
| Shared AP sampling | Local gNMI poller and fsynced WebUI config | System → Live updates, 1–60 seconds | Actual one-second mode produced distinct samples at median 0.997 seconds. A saved two-second setting survived a WebUI service restart; five seconds was restored. |
| Ethernet link and counters | Kernel `/sys/class/net/eth0` / `eth1` | Overview and System → Ethernet ports | Native data corrected the OpenConfig converter mirroring the active port onto the unused one. The actual active port was up at 1 Gbit/s; the unused port had no carrier and zero byte counters. Logical names do not identify physical LAN1/LAN2. |
| VLAN bridges, routes and neighbours | Kernel bridge membership, `/proc/net/vlan/config`, structured `ip` output | Diagnostics → Network paths | All five SSIDs mapped to the expected native/tagged bridges. The management route was present but its unplugged gateway had an unresolved neighbour entry. IPv4/IPv6 routes are read; redundant link-local IPv6 routes are omitted. |
| Wireless event history | Bounded reads of native hostapd logs and rotated history | Diagnostics → Events, search, type filters and text export | Live connection/disconnection and channel-change history returned. Parser tests cover radar events, rotation/deduplication, the latest-150 limit, oversized files and exclusion of secrets/raw log tails. Actual radar detection was not induced. |
| Persistent MAC allow/deny lists | Legacy MAC helper, driver `get_maccmd` / `getmac`, native profiles and TPM-encrypted AP configuration | Wireless networks → Client access; API `GET/PUT /ssids/{name}/policy` | Allow, deny, off, same-mode address replacement and 128 entries independently read back on 2.4, 5 and 6 GHz. Invalid requests leave the policy unchanged. Manager restart and an AP recovery reboot retained desired settings. Final policy saves use live setters rather than VAP restarts. |
| Client limits per band | Driver `maxsta` / `get_maxsta`, native association-limit fields | Client access → Limit clients per band | Limits 4, 5, 6, 7 and 8 read back independently on all three bands; null restored 127. This is a limit per BSS, not a total across bands. Admission with 127 real clients was not load-tested. |

## Persistence and limits

Client access settings have a separate, atomically saved desired file at `/opt/c460-webui/ssid-policies.json` (0600). The implementation modifies only five admission fields in the native AP configuration and the corresponding runtime profile fields. It prepares and decrypts a TPM-encrypted copy for byte comparison before publishing changes, preserves the native `ap.conf` symlink, and detects competing native writers. The vendor MAC helper and driver client-limit setter apply changes without rebuilding VAPs. Limits alone preserve an unchanged operating MAC list. Independent getters must match before desired settings are committed; failed writes, driver verification or desired-state persistence attempt rollback. Automated tests cover encryption mismatch, native-file races, file/driver rollback, restart persistence, renamed/deleted policies, disabled networks, multi-network restore and runtime drift repair.

During the initial implementation, generic VAP MOD operations contributed to repeated 6 GHz beacon loss; the firmware reboot log recorded `consecutive vap restart due to vap down`. The AP recovered with the installation and saved networks intact. The final implementation avoids this route for policy-only writes; allow/deny/off, address replacement and limits were retested on all three bands without an AP restart. Ordinary network/radio changes still use the vendor configuration path. Disabled networks retain pending desired settings without starting wireless interfaces. Wireless backups include policies and off defaults; older backups without this field preserve policies on retained networks.

NTP writes preserve all other native sensor fields, encrypt a prepared copy before modifying the live files, and roll back if saving or restarting NTP fails. A separately saved desired configuration under `/opt/c460-webui/` is restored at web-service startup. Concurrent native changes detected during encryption cause the save to be refused. No deliberate full AP reboot or physical power cycle was performed for these additions; the encrypted native boot configuration and service restart were verified. Clock synchronisation could not be tested without upstream NTP connectivity.

Wireless feature updates merge into the existing SSID configuration, preserving security, credentials, VLANs and other advanced leaves. The firmware applies them through its existing OpenConfig path. The advanced dialog distinguishes explicitly enabled/disabled settings from absent leaves and displays the operating driver values separately. Enabling these advertisements does not guarantee a particular phone will roam differently.

LLDP timing uses an atomically written, fsynced desired file under `/opt/c460-webui/`. Failed native writes or failed saving roll back the previous timing. The service restores explicit desired timing at startup and checks every 30 seconds, because the native daemon can restart independently. These additions passed the boot-trust check; full AP reboot/power-cycle verification was not performed in this round. The daemon already runs despite `lldp_enabled=0` in the native sensor file. Its operation and PoE negotiation remain vendor-managed; this UI controls timing only. A hostname command was accepted but did not change the advertised System Name, so it is not exposed.

Authenticated endpoints are `GET/PUT /api/ssids/{name}/features` (nullable boolean `rrm` and `load`, both required on save), `GET/PUT /api/lldp` (integer `interval` and `hold`), and `POST /api/diagnostics` (`tool: "tcp"`, target and port). Readback uses fixed native tools and structured JSON or command-specific values.

Diagnostics execute only fixed utilities with validated hostname/IP targets, authenticated sessions, a single active diagnostic, a 15-second deadline and capped output. They run from the management network; there is no arbitrary command endpoint or automatic switch/router modification. Hostapd commands use short-lived private Unix socket paths, deadlines and allowlisted response fields. Reconnect checks the station is currently associated before sending the command.

## Bounded packet capture and support bundles

The WebUI exposes these under **Diagnostics → Capture / Support**, and the API exposes `/api/captures` jobs/downloads and `/api/support-bundle` (also under `/api/v1`). Capture uses the installed `/sbin/tcpdump` with separate fixed argv, structured IP/protocol/port filters, packet-buffered classic PCAP output, and no promiscuous or monitor mode. A streaming framer validates PCAP headers/record lengths and writes complete records only, enforcing an exact 8 MiB maximum per file. Duration is at most 120 seconds. Only one capture runs, at most three jobs remain in private `/tmp` storage, and finished files expire after ten minutes. Cancellation/deadlines interrupt tcpdump, force-kill after a two-second grace period, and preserve only complete packets. Linux sends a parent-death signal if the manager crashes; startup removes previous capture files. No radio or VAP restart is requested.

Starting/stopping/deleting captures requires administrator access and API `control` + `secrets` permissions; downloading PCAPs requires administrator access and `secrets`. Metadata is monitor-readable. Packet contents can contain credentials and are never included in support bundles. Captures observe only data visible to kernel interfaces: hardware offload can hide traffic, bridges can duplicate it, and raw 802.11/radiotap capture is not exposed.

Support bundles use an independent typed allowlist of cached status and bounded fresh network/hostapd/parsed-event reads. Credentials, raw native configuration, raw log/crash contents, arbitrary gNMI maps, account usernames and packet files are excluded. Client details are optional and off by default. The ZIP includes a manifest with inclusion choices, timestamps and collection warnings. It has a 15-second collection deadline and a 2 MiB uncompressed/archive cap, and does not write a persistent copy on the AP. Viewers and monitor tokens may download this diagnostic data, which still contains AP/network identifiers.

Automated local tests cover exact record-boundary caps with both endian and timestamp formats, malformed/partial PCAP input, short writes, validation/argv restrictions, cancellation/timeouts, concurrent admission, retention/expiry/shutdown cleanup, private permissions, failed-tool output suppression, authenticated binary downloads, token scopes, support redaction/client opt-in and OpenAPI contracts. Live tests on C-460 firmware 18.2.0-32 and tcpdump 4.99.1/libpcap 1.10.1 confirmed loopback UDP filtering, deadline and record-boundary size stops, cancellation, valid native PCAP reading, private file permissions, token restrictions, bundle selections and credential-field exclusion. Browser checks confirmed capture start, packet counters, stop and support-download success. The browser-created capture and its file were absent after expiry. A forced manager SIGKILL stopped its tcpdump child, the manager recovered, and startup cleared the capture files. The original six networks/16 enabled BSSs and native EHT320 remained intact; native configuration and encrypted copies were unchanged and the AP did not reboot. Actual wireless forwarding visibility, saturation throughput and precise wall-clock expiry timing remain unmeasured; expiry and retained-job bounds have automated coverage. No raw-radio/monitor capture or ordinary user-traffic capture was used for validation.

## Bandwidth limits and QoS

Wireless networks → Bandwidth and QoS and Clients → View → Bandwidth limits
expose a shared SSID upload/download cap, a default cap for each connected
device, and explicit Wi-Fi MAC overrides. The API provides GET/PUT traffic and
PUT/DELETE client overrides, with monitor/configure token permissions and
complete OpenAPI schemas. Rates use decimal Kbps (32–10,000,000); blank/null
means unlimited. Overrides replace both default directions; aggregate caps
still apply. Eight native IFB queue pairs are available on this device.

The earlier 1 Gbps ceiling was software validation. On firmware 18.2.0-32,
isolated native TBF and HTB queues accepted 10,000,000 Kbps and reported
1,250,000,000 bytes/second for both rate and ceiling. The vendor burst
calculation also accepted this value. The temporary IFB interface was removed
after verification. This verifies queue configuration, not 10 Gbps throughput;
actual speed depends on the Wi-Fi link, Ethernet uplink and shaping load.

The backend uses the installed tc_wrapper.sh path, IFB/TBF/HTB queues and
vendor WMM set_qos setters. Desired settings live in traffic-policies.json
(0600), with atomic persistence and runtime rollback. It does not edit native
ap.conf or its encrypted copy for these controls and does not restart radios.
The verifier checks kernel rates, complete Ethernet MAC matches, both IPv4
and IPv6 redirection paths, and removal of old queues/redirection. iproute2
emits duplicate JSON match keys for u32 filters: both must be preserved.
SSID TBF queues expose a virtual class 1:1, which is explicitly distinguished
from unexpected client classes. Inactive networks save pending settings;
renames/deletions release old profile queues before their mapping disappears.

A real phone on a 6 GHz test network measured 902.67 Mbps download and 318.49
Mbps upload without caps. A 4,000 Kbps down / 1,000 Kbps up SSID cap measured
3.69 / 0.42 Mbps; a MAC-only cap measured 3.76 / 0.70 Mbps. Browser uploads
count only completed requests, so low-rate upload results omit transfers
still in flight at the timer boundary. Native client queues recorded 1,284
download drops / 12,968 overlimits and 220 upload drops / 3,684 overlimits
after the test sequence. These are cumulative observations, not drop ratios
or guaranteed throughput. IPv4 forwarding was measured; IPv6 filters were
verified but IPv6 payload throughput was not measured.

Fixed voice priority with upstream DSCP marking produced IPv4 TOS 0xb8
(DSCP 46) in metadata-only captures of the phone's packets on eth0 under its
cap. Voice/video/best-effort/background, ceiling/fixed, DSCP/802.1p/TOS
mapping and upstream marking are configurable. The driver has no exact QoS
operating getter; the API/UI explicitly distinguish accepted configuration
from operating readback. Relative traffic-priority performance under
competing wireless loads remains unmeasured. Rate caps do not simulate weak
RF, injected delay, jitter or packet loss.

Live API checks verified shared caps, defaults, device overrides, unlimited
exemption, removal/inheritance, configure-token restrictions, invalid input,
backup inclusion, kernel drift repair, and manager restart persistence with
QoS reapplication. The phone stopped returning later measurements; those
additional API-driven speed runs were skipped. Browser checks also saved a 2,000/500 Kbps device override, displayed verified
kernel queues, then removed the override and confirmed no active queues. Temporary caps were removed.
The original six networks, 16 enabled BSSs, EHT320 and native configuration
hashes remained intact, with no AP reboot. Full power-cycle, multiple-client
contention and simultaneous traffic across bands were not tested in this round.
Automated tests cover normalization, caller-owned data isolation, MAC-filter
readback, defaults/overrides, rollback, persistence, pending state, rename/delete,
permissions, OpenAPI bounds and backup validation.

## More installed capabilities requiring separate integration

The firmware contains additional radio, multicast, RADIUS, tunnel, BLE and telemetry machinery. Earlier native probes demonstrated selected WNM/DTIM/BSS-color controls, but runtime acceptance alone does not establish persistence or working client behavior. OpenConfig regeneration can overwrite native radio/profile edits, and some newer configuration-manager handlers are no-ops. These settings need per-feature desired-state merging, operating-state readback and appropriate client/peer tests before appearing as supported controls.

The hidden dual-uplink machinery is present, but physical uplink changes can reboot the AP. IPv6 management is also represented in native network files. Neither is exposed as a new writable control in this release. Root access did not reveal a single supported switch that enables the complete vendor controller feature set.


## Refresh performance and data freshness

The browser follows the shared AP sampling interval. Polling a five-second snapshot once per second would not provide one-second telemetry, so no such default is used. Browser requests are deduplicated, pause while the page is hidden, resume immediately on visibility and back off during failures. They read the shared backend snapshot; opening another browser does not create another gNMI polling loop. Native hardware/management CLI information is refreshed separately every five minutes or after relevant saves. Wireless event history uses a separate five-second cache/visible refresh; network paths are read on opening/manual refresh and cached for up to five seconds.

A short idle/test-environment comparison using aggregate `/proc/stat` over 30 seconds per setting measured 9.68% CPU busy at five-second AP sampling and 23.58% at one second. These are observations on this firmware/device, not capacity guarantees or Wi-Fi throughput benchmarks. An earlier additional one-Hz probe alongside the existing five-second service measured 8.91% versus 26.57%, with median full gNMI read duration 747 ms, 95th percentile 1039 ms and maximum 1222 ms. A narrower per-radio state-path read was rejected by this agent; an efficient smaller telemetry path has not been established. The default remains five seconds. Users can choose one second when they need it; reads never overlap and slow responses can extend the actual interval. Live API verification found median fresh-sample spacing 0.997 seconds and cached HTTP response time about 5.5 ms during one-second mode.

Refresh settings use an atomic rename with file and directory fsync under `/opt/c460-webui/`, preserve API credentials/display labels, retain mode 0600 and roll back the in-memory value if saving fails. Authentication, validation, independent Ethernet counters, mixed bridge membership, safe event parsing, race checks and browser cadence/session/visibility tests are automated. Full AP reboot or physical power-cycle testing was not performed in this round.

New authenticated endpoints are `PUT /api/refresh` (`seconds`, integer 1–60), `GET /api/network` and `GET /api/events`. Event times are the AP's local clock, which was unsynchronised in the router-free test environment. SSID names on historical events use current interface mappings and may differ after interface reuse. Only fixed event types and structured station/frequency fields are returned; raw log lines, keys and passwords are excluded.


## 6 GHz channel-width limitation

On firmware 18.2.0-32, OpenConfig rejects `channel-width: 320` because its schema declares a numeric range of 0–255. Ordinary radio controls and staged changes retain widths up to 160 MHz. The separate **Radios → 6 GHz → Wi-Fi 7** control supports EHT at 160 or 320 MHz through the firmware's native configuration manager; no firmware patch or controller is required.

The adapter discovers the radio-v2 profile by its 6 GHz band and changes only `WIRELESS_PROTOCOL` (4 = HE, 5 = EHT) and `AP_CHAN_WIDTH` (4 = 160, 6 = 320). It dry-runs the vendor diff, rejects changes beyond that radio or requiring reboot, and atomically queues a private candidate under `/tmp/trigger/ap-conf.*`. The firmware manager performs the actual radio restart and native configuration persistence. Successful saves require both actual driver mode (`11AEHT160` / `11AEHT320`) and the kernel operating width to match. A desired-mode setter returning success is insufficient. Failed application or desired-state saving attempts to restore and verify the previous radio settings.

Desired settings are written atomically with fsync and mode 0600 to `/opt/c460-webui/wifi7.json`, included in configuration backups, and reapplied after OpenConfig writes, schedules, restores and service startup. A five-second native observer also detects drift; failures back off for a minute. All WebUI radio writes share the existing AP write lock. No measured vendor executables or schema files are modified.

This setting affects **every BSS on the 6 GHz radio**. A 6 GHz-only SSID prevents a phone from falling back to 5 GHz; it does not have a separate channel width. Existing networks retain their selected bands and VLANs. Disabling Wi-Fi 7 returns the radio to the ordinary OpenConfig Wi-Fi 6E width. Wi-Fi 7/320 capability and negotiated speed depend on the client; MLO and application throughput are not established by this radio-mode test. The Ethernet uplink can also limit a wired speed test independently of the wireless link.

Live verification on 2026-10-03 also confirmed backup export records Wi-Fi 7/320 separately from the schema-safe OpenConfig width, and invalid width/non-6 GHz requests are refused before changes. The complete AP startup took about four minutes in the router-free test environment; the HTTP service appeared before the vendor OpenConfig/radio services were ready. A physical power removal/reconnection and negotiated phone throughput were not tested.

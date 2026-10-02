# Native features on C-460 firmware 18.2.0-32

Root access provides additional interfaces beyond this firmware's OpenConfig converter. This is a record of observed behavior, not a claim that every installed feature works locally.

| Feature | Device interface | WebUI integration | Verification |
|---|---|---|---|
| Primary / secondary NTP server | Native `sensor.conf` keys `ntpserver` / `secntpserver`, TPM-backed encrypted copy, vendor `ntpd.init` | System → Time synchronisation | Temporary secondary server saved and read back; running ntpd arguments checked; survived a WebUI service restart; original servers restored. Encrypted copy was decrypted privately and compared with plaintext. |
| Ping and route tracing | BusyBox, separate argument array | Diagnostics → Network tests | Loopback ping and traceroute succeeded on the AP. Targets with shell syntax rejected. |
| DNS lookup | BusyBox nslookup, management DNS | Diagnostics → Network tests | Actual AP result displayed correctly. DNS lookup failed in the router-free test environment; successful upstream resolution remains dependent on a reachable DNS server. |
| Live BSS status | Hostapd Unix datagram control sockets | Diagnostics → Live wireless interfaces | All 15 interfaces returned ENABLED, BSSID, SSID, frequency, beacon and DTIM values. Actual DTIM was 2, while a native profile file declared 1: operating-state reads matter. |
| Client association details | Hostapd `STA <mac>` | Clients → View | Socket protocol and allowlisted properties tested. AP rejected absent clients. No associated station was present for live client-detail validation during this run. |
| Reconnect client | Hostapd `DEAUTHENTICATE <mac> reason=2` | Clients → View → Reconnect client | Backend tested against a local protocol fixture for success, failure and absent stations; confirmation and success UI tested with a simulated client. Live disconnect/reassociation still requires a connected test station. |

## Persistence and limits

NTP writes preserve all other native sensor fields, encrypt a prepared copy before modifying the live files, and roll back if saving or restarting NTP fails. A separately saved desired configuration under `/opt/c460-webui/` is restored at web-service startup. Concurrent native changes detected during encryption cause the save to be refused. No deliberate full AP reboot or physical power cycle was performed for these additions; the encrypted native boot configuration and service restart were verified. Clock synchronisation could not be tested without upstream NTP connectivity.

Diagnostics execute only fixed utilities with validated hostname/IP targets, authenticated sessions, a single active diagnostic, a 15-second deadline and capped output. They run from the management network; there is no arbitrary command endpoint or automatic switch/router modification. Hostapd commands use short-lived private Unix socket paths, deadlines and allowlisted response fields. Reconnect checks the station is currently associated before sending the command.

## More installed capabilities requiring separate integration

The firmware contains additional radio, QoS, MAC policy, multicast, RADIUS, tunnel, BLE and telemetry machinery. Earlier native probes demonstrated selected WNM/DTIM/BSS-color controls, but runtime acceptance alone does not establish persistence or working client behavior. OpenConfig regeneration can overwrite native radio/profile edits, and some newer configuration-manager handlers are no-ops. These settings need per-feature desired-state merging, operating-state readback and appropriate client/peer tests before appearing as supported controls.

The hidden dual-uplink machinery is present, but physical uplink changes can reboot the AP. IPv6 management is also represented in native network files. Neither is exposed as a new writable control in this release. Root access did not reveal a single supported switch that enables the complete vendor controller feature set.

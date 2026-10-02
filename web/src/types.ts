// Mirrors the JSON produced by the Go backend (state.go / system.go).

export type Band = '2.4' | '5' | '6'
export type OpMode = 'WPA3_SAE' | 'WPA2_PERSONAL' | 'ENHANCED_OPEN' | 'OPEN' | string

export interface Device {
  hostname: string
  siteName: string
  model: string
  firmware: string
  mac: string
  mgmtIp: string
  mgmtPrefix: number
  gateway: string
  country: string
  uptimeSeconds: number
  load: string[]
  memTotal: number
  memAvailable: number
  storageTotal: number
  storageFree: number
  temperatureC: number | null
  sshEnabled: boolean
  uiVersion: string
}

export interface Radio {
  id: number
  band: Band
  frequency: string
  enabled: boolean
  channel: number
  width: number
  powerRequested: number
  eirp: number | null
  maxEirp: number | null
  maxTxPower: number | null
  allowedChannels: number[]
  utilization: number | null
  rxUtilization: number | null
  txUtilization: number | null
  noiseFloor: number | null
  dca: boolean
  dtp: boolean
  scanning: boolean
  baseMac: string
  clients: number
  bssids: number
  neighbors: number
}

export interface Bssid {
  bssid: string
  radioId: number
  band: Band
  clients: number
}

export interface Ssid {
  name: string
  enabled: boolean
  hidden: boolean
  opmode: OpMode
  bands: Band[]
  vlan: number | null
  isolation: boolean
  mfp: boolean
  hasPassword: boolean
  bssids: Bssid[]
  clients: number
  rxBytes: number
  txBytes: number
}

export interface Client {
  mac: string
  ssid: string
  band: Band | ''
  vlan: number | null
  ipv4: string
  ipv4Source?: 'arp'
  ipv6: string[]
  hostname: string
  os: string
  username: string
  state: string
  mode: string
  rssi: number | null
  snr: number | null
  txRate: number | null
  rxRate: number | null
  streams: number | null
  connectedSince: string | null
  rxBytes: number
  txBytes: number
  retries: number
}

export interface Neighbor {
  radioId: number
  band: Band
  bssid: string
  ssid: string
  channel: number
  primaryChannel: number
  rssi: number | null
  opmode: string
  lastSeen: string | null
}

export interface Interface {
  name: string
  up: boolean
  speed: string
  duplex: string
  mac: string
  inOctets: number
  outOctets: number
  inErrors: number
  outErrors: number
  inDiscards: number
  outDiscards: number
  /** Physical socket ETH 1/2 (0 when unknown); the firmware swaps eth0/eth1 so eth0 is always the uplink. */
  port: number
  role: 'uplink' | 'backup' | ''
}

export interface ApState {
  generatedAt: string
  pollSeconds: number
  error?: string
  device: Device
  radios: Radio[]
  ssids: Ssid[]
  clients: Client[]
  neighbors: Neighbor[]
  interfaces: Interface[]
  vlanNames: Record<string, string> | null
  management: Management
  hardware: HardwareInfo
  managementError?: string
}

export interface Management {
  commVlan: string
  mode: 'static' | 'dhcp' | ''
  ipv4: string
  netmask: string
  gateway: string
  dns: string[] | null
  dnsSearch: string
  status: string
  pendingReboot: boolean
}

export interface ManagementInput {
  commVlan: string
  mode: 'static' | 'dhcp'
  ipv4: string
  netmask: string
  gateway: string
  dns: string[]
  dnsSearch: string
}

export interface HardwareInfo {
  serial: string
  powerSource: string
  radioPower: string
  ntpSynced: boolean | null
  lldp: Record<string, string> | null
  updatedAt: string
}

export interface TrustCheck {
  ok: boolean
  problems: string[] | null
}

export interface SsidInput {
  name: string
  enabled: boolean
  hidden: boolean
  opmode: OpMode
  password: string
  bands: Band[]
  vlan: number | null
  isolation: boolean
}

export interface RadioInput {
  enabled: boolean
  channel: number
  width: number
  power: number
  dca: boolean
  dtp: boolean
}

export interface TimeSettings {
  primary: string
  secondary: string
  managed: boolean
  synced: boolean | null
  running: boolean
}
export interface DiagnosticResult {
  port?: number
  tool: string
  target: string
  output: string
  success: boolean
  timedOut: boolean
  durationMs: number
}
export interface WirelessStatus {
  interface: string
  values: Record<string, string>
  error?: string
}
export interface NativeClient {
  interface: string
  values: Record<string, string>
}

export interface SsidFeatures {
  rrm: boolean | null
  load: boolean | null
  interfaces: { interface: string; band: string; rrm: boolean | null; load: boolean | null; error?: string }[]
}
export interface LldpTiming {
  interval: number
  hold: number
}
export interface LldpState extends LldpTiming {
  saved: LldpTiming | null
  neighborError?: string
  neighbors: { interface: string; name: string; description: string; chassisId: string; portId: string; portDescription: string; addresses: string[]; age: string; ttl: string }[]
}

export interface NetworkSnapshot {
  sampledAt: string
  warnings: string[]
  bridges: { name: string; vlan: number | null; vlanMode: string; up: boolean; addresses: string[]; members: string[]; networks: string[] }[]
  routes: { dst: string; gateway: string; dev: string }[]
  neighbors: { dst: string; dev: string; lladdr: string; state: string[]; gateway: boolean }[]
}
export interface WirelessEvent {
  id: string
  time: string
  kind: string
  summary: string
  tone: string
  interface: string
  network: string
  client: string
  frequency: string
}
export interface WirelessEventLog {
  events: WirelessEvent[]
  sampledAt: string
  clockSynced: boolean | null
  error?: string
}

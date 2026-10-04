// Mirrors the JSON produced by the Go backend (state.go / system.go).

export type Band = '2.4' | '5' | '6'
export type OpMode = 'WPA3_SAE' | 'WPA2_WPA3_PERSONAL' | 'WPA2_PERSONAL' | 'ENHANCED_OPEN' | 'OPEN' | string

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
  cpuUsage: number | null
  cpuCores: number
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
  wifi7?: WiFi7State
}

export interface WiFi7Settings {
  enabled: boolean
  width: number
}

export interface WiFi7State {
  supported: boolean
  saved: WiFi7Settings | null
  operatingMode: string
  operatingWidth: number
  status: 'off' | 'pending' | 'active' | 'error'
  error?: string
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
  /** WPA2/WPA3 mixed only: 'applied', 'pending' or a problem description. */
  mixedStatus?: string
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
  health: HealthItem[]
  timeZone: string
  schedules: Record<string, ScheduleStatus> | null
}

export interface HealthItem {
  id: string
  severity: 'danger' | 'warn' | 'info'
  title: string
  detail: string
  link?: string
}

export interface ScheduleWindow {
  /** 0 = Sunday … 6 = Saturday */
  days: number[]
  start: string
  end: string
}

export interface SsidSchedule {
  enabled: boolean
  windows: ScheduleWindow[]
}

export interface ScheduleStatus extends SsidSchedule {
  active: boolean
  next?: string
  waiting?: string
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

export type FeatureValue = boolean | number | null
export type FeatureSettings = Record<string, FeatureValue>

export interface SsidFeatures {
  rrm: boolean | null
  load: boolean | null
  /** Every advanced setting; null means the firmware default. */
  settings: FeatureSettings
  /** What the firmware's own configuration currently runs. */
  native: Record<string, boolean | number>
  interfaces: { interface: string; band: string; rrm: boolean | null; load: boolean | null; error?: string }[]
}

export interface RadioFeatures {
  settings: FeatureSettings
  native: Record<string, boolean | number>
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
  /** ISO timestamp; shown in the browser's local time like the change log. */
  at?: string
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

/** Portable configuration backup produced by POST /api/backup. */
export interface ConfigBackup {
  format: 'c460-webui-backup'
  version: number
  createdAt: string
  source: { model: string; hostname: string; firmware: string; ui: string }
  ssids: { name: string; enabled: boolean; hidden: boolean; opmode: string; bands: Band[]; vlan: number | null; isolation: boolean }[]
  radios: { band: Band; enabled: boolean; channel: number; width: number; power: number; dca: boolean; dtp: boolean }[]
  management?: ManagementInput
  wifi7?: WiFi7Settings
  labels: { siteName: string; vlanNames: Record<string, string> | null }
  time?: { primary: string; secondary: string }
  lldp?: { interval: number; hold: number }
  secrets?: unknown
}

export interface RestoreSections {
  wireless: boolean
  radios: boolean
  management: boolean
  labels: boolean
  time: boolean
  lldp: boolean
}

export interface RestoreResult {
  ok: boolean
  applied: string[]
  problems: string[] | null
  rebootRequired: boolean
}

export interface SnmpSettings {
  enabled: boolean
  community: string
  location: string
  contact: string
}

export interface SnmpStatus extends SnmpSettings {
  running: boolean
  error?: string
}

export type Role = 'admin' | 'viewer' | ''

export interface Session {
  authenticated: boolean
  configured: boolean
  username: string
  role: Role
  /** Read-only account name, only reported to administrators. */
  viewer?: string
}

export interface HistoryPoint {
  t: number
  clients: number
  perSsid: Record<string, number>
  util: Record<string, number>
  rxBps: number | null
  txBps: number | null
  tempC: number | null
}

export interface ClientSample {
  t: number
  rssi: number | null
  band: Band | ''
  ssid: string
  txRate: number | null
  rxRate: number | null
}

export interface ChangeEntry {
  time: string
  user: string
  address: string
  action: string
  ok: boolean
  error?: string
}

export interface MetricsSettings {
  enabled: boolean
  token: string
}

export interface JoinCode {
  ssid: string
  opmode: OpMode
  password: string
  hidden: boolean
  payload: string
}

export type StagedChange =
  | { kind: 'ssid-create'; ssid: SsidInput }
  | { kind: 'ssid-update'; name: string; ssid: SsidInput }
  | { kind: 'ssid-delete'; name: string }
  | { kind: 'radio'; id: number; radio: RadioInput }
export type ApiScope = 'monitor' | 'configure' | 'control' | 'secrets'
export interface AccessToken {
  id: string
  name: string
  scopes: ApiScope[]
  createdAt: string
  expiresAt: string | null
}

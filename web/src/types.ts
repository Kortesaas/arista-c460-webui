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

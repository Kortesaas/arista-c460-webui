import type {
  AccessToken,
  ApiScope,
  ApState,
  ManagementInput,
  RadioInput,
  WiFi7Settings,
  WiFi7State,
  SsidInput,
  SsidPolicy,
  SsidPolicyStatus,
  TrafficPolicy,
  TrafficLimits,
  TrafficStatus,
  TrustCheck,
  TimeSettings,
  DiagnosticResult,
  WirelessStatus,
  NativeClient,
  SsidFeatures,
  LldpState,
  LldpTiming,
  SnmpSettings,
  SnmpStatus,
  NetworkSnapshot,
  WirelessEventLog,
  ConfigBackup,
  RestoreSections,
  RestoreResult,
  Session,
  HistoryPoint,
  ClientSample,
  ChangeEntry,
  MetricsSettings,
  JoinCode,
  SsidSchedule,
  ScheduleStatus,
  StagedChange,
  FeatureSettings,
  RadioFeatures,
  CaptureInput,
  CaptureJob,
  CaptureStatus,
} from '@/types'

export class ApiError extends Error {
  constructor(
    message: string,
    readonly status: number,
  ) {
    super(message)
  }
}

async function call<T>(method: string, path: string, body?: unknown): Promise<T> {
  const headers: Record<string, string> = { 'X-Requested-With': 'c460-webui' }
  if (body !== undefined) headers['Content-Type'] = 'application/json'
  const response = await fetch(path, {
    method,
    headers,
    credentials: 'same-origin',
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  const data = (await response.json().catch(() => ({}))) as { error?: string }
  if (!response.ok) throw new ApiError(data.error ?? `${response.status} ${response.statusText}`, response.status)
  return data as T
}

async function download(method: 'GET' | 'POST', path: string, name: string, body?: unknown) {
  const response = await fetch(path, {
    method, credentials: 'same-origin',
    headers: { 'X-Requested-With': 'c460-webui', ...(body === undefined ? {} : { 'Content-Type': 'application/json' }) },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  if (!response.ok) {
    const error = (await response.json().catch(() => ({}))) as { error?: string }
    throw new ApiError(error.error ?? `${response.status} ${response.statusText}`, response.status)
  }
  const url = URL.createObjectURL(await response.blob())
  const link = document.createElement('a')
  link.href = url
  link.download = name
  document.body.appendChild(link)
  link.click()
  link.remove()
  setTimeout(() => URL.revokeObjectURL(url), 1000)
}

export const api = {
  traffic: (name: string) => call<TrafficStatus>('GET', `/api/ssids/${encodeURIComponent(name)}/traffic`),
  updateTraffic: (name: string, policy: TrafficPolicy) => call<TrafficStatus>('PUT', `/api/ssids/${encodeURIComponent(name)}/traffic`, policy),
  updateClientTraffic: (name: string, mac: string, limits: TrafficLimits) => call<TrafficStatus>('PUT', `/api/ssids/${encodeURIComponent(name)}/traffic/clients/${encodeURIComponent(mac)}`, limits),
  removeClientTraffic: (name: string, mac: string) => call<TrafficStatus>('DELETE', `/api/ssids/${encodeURIComponent(name)}/traffic/clients/${encodeURIComponent(mac)}`),
  captures: () => call<CaptureStatus>('GET', '/api/captures'),
  startCapture: (input: CaptureInput) => call<CaptureJob>('POST', '/api/captures', input),
  stopCapture: (id: string) => call<CaptureJob>('POST', `/api/captures/${encodeURIComponent(id)}/stop`, {}),
  deleteCapture: (id: string) => call('DELETE', `/api/captures/${encodeURIComponent(id)}`),
  downloadCapture: (id: string) => download('GET', `/api/captures/${encodeURIComponent(id)}/download`, `capture-${id}.pcap`),
  supportBundle: (includeClients: boolean, includeEvents: boolean) => download('POST', '/api/support-bundle', `c460-support-${new Date().toISOString().slice(0, 10)}.zip`, { includeClients, includeEvents }),
  tokens: () => call<{ tokens: AccessToken[] }>('GET', '/api/tokens'),
  createToken: (name: string, scopes: ApiScope[], expiresDays: number) => call<{ token: string; access: AccessToken }>('POST', '/api/tokens', { name, scopes, expiresDays }),
  revokeToken: (id: string) => call('DELETE', `/api/tokens/${encodeURIComponent(id)}`),
  backup: (passphrase: string) => call<ConfigBackup>('POST', '/api/backup', { passphrase }),
  restore: (backup: ConfigBackup, passphrase: string, sections: RestoreSections, removeOthers: boolean, management?: ManagementInput) =>
    call<RestoreResult>('POST', '/api/restore', { backup, passphrase, sections, removeOthers, management }),
  session: () => call<Session>('GET', '/api/session'),
  login: (username: string, password: string) => call<{ authenticated: boolean; role: Session['role'] }>('POST', '/api/login', { username, password }),
  setViewer: (username: string, password: string) => call<{ viewer: string }>('PUT', '/api/viewer', { username, password }),
  removeViewer: () => call<{ viewer: string }>('DELETE', '/api/viewer'),
  history: (hours: number) => call<{ points: HistoryPoint[]; intervalSeconds: number }>('GET', `/api/history?hours=${hours}`),
  clientHistory: (mac: string) => call<{ samples: ClientSample[] }>('GET', `/api/clients/${encodeURIComponent(mac)}/history`),
  changes: () => call<{ entries: ChangeEntry[]; limit: number }>('GET', '/api/changes'),
  metrics: () => call<MetricsSettings>('GET', '/api/metrics'),
  updateMetrics: (enabled: boolean, newToken: boolean) => call<MetricsSettings>('PUT', '/api/metrics', { enabled, newToken }),
  joinCode: (name: string) => call<JoinCode>('GET', `/api/ssids/${encodeURIComponent(name)}/join`),
  updateSchedule: (name: string, schedule: SsidSchedule) => call<{ schedule: ScheduleStatus }>('PUT', `/api/ssids/${encodeURIComponent(name)}/schedule`, schedule),
  updateTimeZone: (timeZone: string) => call<{ timeZone: string }>('PUT', '/api/timezone', { timeZone }),
  applyBatch: (changes: StagedChange[]) => call('POST', '/api/batch', { changes }),
  logout: () => call('POST', '/api/logout', {}),
  changeCredentials: (current: string, username: string, next: string) => call('POST', '/api/password', { current, username, next }),
  updateRefresh: (seconds: number) => call('PUT', '/api/refresh', { seconds }),
  network: () => call<NetworkSnapshot>('GET', '/api/network'),
  events: () => call<WirelessEventLog>('GET', '/api/events'),
  state: () => call<ApState>('GET', '/api/state'),
  createSsid: (input: SsidInput) => call('POST', '/api/ssids', input),
  updateSsid: (name: string, input: SsidInput) => call('PUT', `/api/ssids/${encodeURIComponent(name)}`, input),
  deleteSsid: (name: string) => call('DELETE', `/api/ssids/${encodeURIComponent(name)}`),
  ssidFeatures: (name: string) => call<SsidFeatures>('GET', `/api/ssids/${encodeURIComponent(name)}/features`),
  updateSsidFeatures: (name: string, settings: FeatureSettings) => call('PUT', `/api/ssids/${encodeURIComponent(name)}/features`, settings),
  ssidPolicy: (name: string) => call<SsidPolicyStatus>('GET', `/api/ssids/${encodeURIComponent(name)}/policy`),
  updateSsidPolicy: (name: string, policy: SsidPolicy) => call<SsidPolicyStatus>('PUT', `/api/ssids/${encodeURIComponent(name)}/policy`, policy),
  radioFeatures: (id: number) => call<RadioFeatures>('GET', `/api/radios/${id}/features`),
  updateRadioFeatures: (id: number, settings: FeatureSettings) => call('PUT', `/api/radios/${id}/features`, settings),
  lldp: () => call<LldpState>('GET', '/api/lldp'),
  updateLldp: (timing: LldpTiming) => call('PUT', '/api/lldp', timing),
  snmp: () => call<SnmpStatus>('GET', '/api/snmp'),
  updateSnmp: (settings: SnmpSettings) => call<SnmpStatus>('PUT', '/api/snmp', settings),
  updateRadio: (id: number, input: RadioInput) => call('PUT', `/api/radios/${id}`, input),
  updateWiFi7: (id: number, input: WiFi7Settings) => call<WiFi7State>('PUT', `/api/radios/${id}/wifi7`, input),
  updateManagement: (input: ManagementInput) => call<{ ok: boolean; changed: boolean; rebootRequired: boolean }>('PUT', '/api/management', input),
  updateSettings: (siteName: string, vlanNames: Record<string, string>) => call('PUT', '/api/settings', { siteName, vlanNames }),
  updateSSH: (enabled: boolean) => call('PUT', '/api/ssh', { enabled }),
  trust: () => call<TrustCheck>('GET', '/api/trust'),
  reboot: () => call('POST', '/api/reboot', {}),
  locate: (minutes: number) => call('POST', '/api/locate', { minutes }),
  time: () => call<TimeSettings>('GET', '/api/time'),
  updateTime: (primary: string, secondary: string) => call('PUT', '/api/time', { primary, secondary }),
  diagnose: (tool: string, target: string, port?: number) => call<DiagnosticResult>('POST', '/api/diagnostics', { tool, target, port }),
  wirelessStatus: () => call<{ interfaces: WirelessStatus[]; sampledAt: string }>('GET', '/api/wireless-status'),
  clientDetails: (mac: string) => call<NativeClient>('GET', `/api/clients/${encodeURIComponent(mac)}/details`),
  reconnectClient: (mac: string) => call('POST', `/api/clients/${encodeURIComponent(mac)}/reconnect`, {}),
  stopLocate: () => call('DELETE', '/api/locate'),
}

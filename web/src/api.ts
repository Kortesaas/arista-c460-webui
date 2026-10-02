import type {
  ApState,
  ManagementInput,
  RadioInput,
  SsidInput,
  TrustCheck,
  TimeSettings,
  DiagnosticResult,
  WirelessStatus,
  NativeClient,
  SsidFeatures,
  LldpState,
  LldpTiming,
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

export const api = {
  session: () => call<{ authenticated: boolean; configured: boolean; username: string }>('GET', '/api/session'),
  login: (username: string, password: string) => call<{ authenticated: boolean }>('POST', '/api/login', { username, password }),
  logout: () => call('POST', '/api/logout', {}),
  changeCredentials: (current: string, username: string, next: string) => call('POST', '/api/password', { current, username, next }),
  state: () => call<ApState>('GET', '/api/state'),
  createSsid: (input: SsidInput) => call('POST', '/api/ssids', input),
  updateSsid: (name: string, input: SsidInput) => call('PUT', `/api/ssids/${encodeURIComponent(name)}`, input),
  deleteSsid: (name: string) => call('DELETE', `/api/ssids/${encodeURIComponent(name)}`),
  ssidFeatures: (name: string) => call<SsidFeatures>('GET', `/api/ssids/${encodeURIComponent(name)}/features`),
  updateSsidFeatures: (name: string, rrm: boolean | null, load: boolean | null) => call('PUT', `/api/ssids/${encodeURIComponent(name)}/features`, { rrm, load }),
  lldp: () => call<LldpState>('GET', '/api/lldp'),
  updateLldp: (timing: LldpTiming) => call('PUT', '/api/lldp', timing),
  updateRadio: (id: number, input: RadioInput) => call('PUT', `/api/radios/${id}`, input),
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

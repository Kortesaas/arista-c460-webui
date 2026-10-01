import type { ApState, RadioInput, SsidInput } from '@/types'

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
  session: () => call<{ authenticated: boolean; configured: boolean }>('GET', '/api/session'),
  login: (password: string) => call<{ authenticated: boolean }>('POST', '/api/login', { password }),
  logout: () => call('POST', '/api/logout', {}),
  changePassword: (current: string, next: string) => call('POST', '/api/password', { current, next }),
  state: () => call<ApState>('GET', '/api/state'),
  createSsid: (input: SsidInput) => call('POST', '/api/ssids', input),
  updateSsid: (name: string, input: SsidInput) => call('PUT', `/api/ssids/${encodeURIComponent(name)}`, input),
  deleteSsid: (name: string) => call('DELETE', `/api/ssids/${encodeURIComponent(name)}`),
  updateRadio: (id: number, input: RadioInput) => call('PUT', `/api/radios/${id}`, input),
}

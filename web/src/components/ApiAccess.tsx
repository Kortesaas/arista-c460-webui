import { useEffect, useState } from 'react'
import { Copy, KeyRound, Plus, RefreshCw, Trash2 } from 'lucide-react'
import { api } from '@/api'
import { useApp } from '@/stores/app'
import type { AccessToken, ApiScope } from '@/types'
import { Badge, Button, Dialog, DialogActions, Field, Input, Panel, Select, Spinner } from '@/ui/kit'

const permissions: { scope: ApiScope; title: string; detail: string }[] = [
  { scope: 'configure', title: 'Configure', detail: 'Wireless, radios, IP, gateway, schedules and services.' },
  { scope: 'control', title: 'Device actions', detail: 'Restart AP, reconnect clients, SSH and locate LEDs.' },
  { scope: 'secrets', title: 'Read secrets', detail: 'Wi-Fi passwords, monitoring secrets and backups.' },
]

export function ApiAccessPanel() {
  const toast = useApp((s) => s.toast)
  const [tokens, setTokens] = useState<AccessToken[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [creating, setCreating] = useState(false)
  const [revoking, setRevoking] = useState<AccessToken | null>(null)
  const [busy, setBusy] = useState(false)
  const load = async () => {
    setLoading(true)
    try { setTokens((await api.tokens()).tokens); setError('') }
    catch (e) { setError(e instanceof Error ? e.message : String(e)) }
    finally { setLoading(false) }
  }
  useEffect(() => { void load() }, [])
  const revoke = async () => {
    if (!revoking) return
    setBusy(true)
    try { await api.revokeToken(revoking.id); setRevoking(null); await load(); toast('API token revoked.', 'ok') }
    catch (e) { toast(e instanceof Error ? e.message : String(e), 'danger') }
    finally { setBusy(false) }
  }
  return <Panel title="API access" help="Give each Raspberry Pi or other integration its own access token. Monitoring is always included; choose extra permissions only when needed. Tokens survive restarts and can be revoked here." actions={<Button size="sm" onClick={() => setCreating(true)} disabled={loading || Boolean(error) || tokens.length >= 32}><Plus size={12} /> Create token</Button>}>
    <p className="text-[12px] leading-5 text-muted">Monitor and configure this AP from another computer on your network.</p>
    <div className="mt-2 flex flex-wrap gap-x-4 gap-y-1 text-[12px]">
      <a className="text-accent hover:underline" href="/api/v1/docs" target="_blank" rel="noreferrer">API guide & examples ↗</a>
      <a className="text-accent hover:underline" href="/api/v1/openapi.json" target="_blank" rel="noreferrer">OpenAPI specification ↗</a>
    </div>
    {loading ? <div className="mt-3 flex items-center gap-2 text-[12px] text-faint"><Spinner size={12} /> Loading tokens…</div> : error ? <div className="mt-3 flex flex-wrap items-center gap-2 text-[12px] text-danger"><span>{error}</span><Button size="sm" onClick={() => void load()}><RefreshCw size={12} /> Retry</Button></div> : tokens.length === 0 ? <p className="mt-3 flex items-center gap-2 text-[12px] text-faint"><KeyRound size={13} /> No integrations connected yet. Create a token to get started.</p> : <ul className="mt-3 divide-y divide-line border-t border-line">
      {tokens.map((token) => {
        const expired = token.expiresAt && new Date(token.expiresAt).getTime() <= Date.now()
        return <li key={token.id} className="flex items-start justify-between gap-3 py-3 last:pb-0">
          <div className="min-w-0">
            <p className="break-words text-[13px] font-medium text-ink">{token.name}</p>
            <div className="mt-1 flex flex-wrap gap-1">{token.scopes.map((scope) => <Badge key={scope}>{scope}</Badge>)}</div>
            <p className="mt-1 text-[11px] text-faint">{expired ? 'Expired' : token.expiresAt ? `Expires ${new Date(token.expiresAt).toLocaleDateString()}` : 'No expiry'} · created {new Date(token.createdAt).toLocaleDateString()}</p>
          </div>
          <Button size="sm" variant="ghost" aria-label={`Revoke ${token.name}`} onClick={() => setRevoking(token)}><Trash2 size={13} /></Button>
        </li>
      })}
    </ul>}
    {creating && <CreateTokenDialog onClose={() => { setCreating(false); void load() }} />}
    {revoking && <Dialog title={`Revoke “${revoking.name}”?`} description="Its integration loses API access immediately. You can create a replacement token later." onClose={() => !busy && setRevoking(null)}><DialogActions><Button disabled={busy} onClick={() => setRevoking(null)}>Cancel</Button><Button variant="danger" disabled={busy} onClick={() => void revoke()}>{busy && <Spinner size={12} />} Revoke token</Button></DialogActions></Dialog>}
  </Panel>
}

function CreateTokenDialog({ onClose }: { onClose: () => void }) {
  const toast = useApp((s) => s.toast)
  const [name, setName] = useState('')
  const [scopes, setScopes] = useState<ApiScope[]>(['monitor'])
  const [days, setDays] = useState('365')
  const [busy, setBusy] = useState(false)
  const [secret, setSecret] = useState('')
  const create = async () => {
    setBusy(true)
    try { setSecret((await api.createToken(name.trim(), scopes, Number(days))).token) }
    catch (e) { toast(e instanceof Error ? e.message : String(e), 'danger') }
    finally { setBusy(false) }
  }
  const copy = async () => {
    if (!navigator.clipboard) { toast('Select the token and copy it manually.'); return }
    try { await navigator.clipboard.writeText(secret); toast('Token copied.', 'ok') }
    catch { toast('Select the token and copy it manually.') }
  }
  return <Dialog title={secret ? 'Save your API token' : 'Create API token'} description={secret ? 'This secret is shown only once. Store it on your integration computer before closing.' : 'Give the integration a name and choose what it can do.'} onClose={() => !busy && onClose()}>
    {secret ? <div className="space-y-3">
      <Field label="Access token"><Input className="mono" value={secret} readOnly onFocus={(e) => e.currentTarget.select()} aria-label="New API token" /></Field>
      <p className="text-[12px] text-muted">Send it with requests as <code className="mono">Authorization: Bearer YOUR_TOKEN</code>.</p>
      <DialogActions><Button onClick={() => void copy()}><Copy size={13} /> Copy token</Button><Button variant="primary" onClick={onClose}>Done</Button></DialogActions>
    </div> : <form className="space-y-3" onSubmit={(e) => { e.preventDefault(); if (name.trim() && !busy) void create() }}>
      <Field label="Integration name"><Input autoFocus maxLength={64} value={name} onChange={(e) => setName(e.target.value)} placeholder="Raspberry Pi monitor" /></Field>
      <div className="rounded border border-line bg-surface-2 p-3">
        <p className="text-[12px] font-medium text-ink">Monitoring included</p><p className="mt-1 text-[11px] text-muted">Status, clients, events, history and connection tests.</p>
        <div className="mt-3 space-y-3">{permissions.map(({ scope, title, detail }) => <label key={scope} className="flex cursor-pointer items-start gap-2 text-[12px]"><input type="checkbox" className="mt-0.5 accent-accent" checked={scopes.includes(scope)} onChange={(e) => setScopes((prev) => e.target.checked ? [...prev, scope] : prev.filter((item) => item !== scope))} /><span><span className="font-medium text-ink">{title}</span><span className="mt-0.5 block text-[11px] text-muted">{detail}</span></span></label>)}</div>
      </div>
      <Field label="Expires after"><Select value={days} onChange={(e) => setDays(e.target.value)}><option value="30">30 days</option><option value="90">90 days</option><option value="365">1 year</option><option value="0">No expiry</option></Select></Field>
      <DialogActions><Button disabled={busy} onClick={onClose}>Cancel</Button><Button type="submit" variant="primary" disabled={busy || !name.trim()}>{busy && <Spinner size={12} />} Create token</Button></DialogActions>
    </form>}
  </Dialog>
}

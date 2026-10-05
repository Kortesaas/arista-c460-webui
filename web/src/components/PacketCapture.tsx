import { useEffect, useRef, useState } from 'react'
import { Download, Play, RefreshCw, Square, Trash2 } from 'lucide-react'
import { api } from '@/api'
import { useApp } from '@/stores/app'
import type { CaptureInput, CaptureJob, CaptureStatus } from '@/types'
import { Badge, Button, Field, Input, Panel, Select, Spinner, Toggle } from '@/ui/kit'

const size = (bytes: number) => bytes < 1024 ? `${bytes} B` : bytes < 1024 * 1024 ? `${(bytes / 1024).toFixed(1)} KiB` : `${(bytes / 1024 / 1024).toFixed(2)} MiB`
const reasons: Record<string, string> = { 'time-limit': 'Duration reached', 'size-limit': 'Size limit reached', cancelled: 'Stopped by user or manager', exited: 'Capture finished', error: 'Capture failed' }

export function CapturePanel() {
  const admin = useApp((s) => s.role === 'admin')
  const toast = useApp((s) => s.toast)
  const [status, setStatus] = useState<CaptureStatus | null>(null)
  const [input, setInput] = useState<CaptureInput>({ interface: '', protocol: 'all', host: '', port: 0, seconds: 10, maxBytes: 1 << 20, snapLength: 128 })
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const pending = useRef(false)
  const load = async () => {
    if (pending.current) return
    pending.current = true
    try {
      const next = await api.captures()
      setStatus(next)
      setInput((old) => old.interface ? old : { ...old, interface: next.interfaces.find((i) => i.name === 'eth0' && i.up)?.name || next.interfaces.find((i) => i.up)?.name || '' })
      setError('')
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally { pending.current = false }
  }
  useEffect(() => {
    void load()
    const tick = setInterval(() => { if (document.visibilityState !== 'hidden') void load() }, 2000)
    return () => clearInterval(tick)
  }, [])
  const set = <K extends keyof CaptureInput>(key: K, value: CaptureInput[K]) => setInput((old) => ({ ...old, [key]: value }))
  const active = status?.jobs.some((job) => job.state === 'running')
  const available = status?.interfaces.some((iface) => iface.name === input.interface && iface.up)
  const action = async (run: () => Promise<unknown>) => {
    setBusy(true)
    try { await run(); await load() } catch (e) { toast(e instanceof Error ? e.message : String(e), 'danger') } finally { setBusy(false) }
  }
  const changeProtocol = (protocol: CaptureInput['protocol']) => setInput((old) => ({ ...old, protocol, port: protocol === 'arp' || protocol === 'icmp' ? 0 : old.port }))
  return (
    <div className="space-y-3">
      <Panel title="Packet capture" help="Captures data visible to the selected interface without changing radio settings. Hardware offload may hide forwarded traffic. Wireless data captures do not include raw 802.11 radio frames.">
        {!status && !error && <p className="text-[12px] text-muted">Reading capture interfaces…</p>}
        {error && <p role="alert" className="mb-3 text-[12px] text-danger">{error}</p>}
        {status && !status.supported && <p role="alert" className="mb-3 text-[12px] text-warn">Packet capture is unavailable on this AP. The capture tool and writable temporary storage are required.</p>}
        {!admin && <p className="mb-3 text-[12px] text-muted">Sign in as administrator to start, stop, download or delete packet captures.</p>}
        <form onSubmit={(e) => { e.preventDefault(); if (admin && !busy && !active && available && status?.supported) void action(() => api.startCapture(input)) }}>
          <fieldset disabled={busy || !admin || !status?.supported} className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
            <Field label="Interface">
              <Select required value={input.interface} onChange={(e) => set('interface', e.target.value)}>
                {!input.interface && <option value="">Choose an interface</option>}
                {status?.interfaces.map((iface) => <option key={iface.name} value={iface.name} disabled={!iface.up}>{iface.name} · {iface.network || iface.kind}{iface.up ? '' : ' · disabled'}</option>)}
              </Select>
            </Field>
            <Field label="Traffic">
              <Select value={input.protocol} onChange={(e) => changeProtocol(e.target.value as CaptureInput['protocol'])}>
                <option value="all">All protocols</option><option value="arp">ARP</option><option value="icmp">ICMP / ICMPv6</option><option value="tcp">TCP</option><option value="udp">UDP</option>
              </Select>
            </Field>
            <Field label="Host IP" hint="Optional. Matches source or destination.">
              <Input value={input.host} maxLength={45} placeholder="IPv4 or IPv6 address" spellCheck={false} onChange={(e) => set('host', e.target.value)} />
            </Field>
            <Field label="Port" hint="Optional. Matches source or destination.">
              <Input type="number" min={1} max={65535} value={input.port || ''} disabled={input.protocol === 'arp' || input.protocol === 'icmp'} placeholder="Any port" onChange={(e) => set('port', e.target.value ? Number(e.target.value) : 0)} />
            </Field>
            <Field label="Duration (seconds)">
              <Input type="number" min={1} max={120} required value={input.seconds || ''} onChange={(e) => set('seconds', Number(e.target.value))} />
            </Field>
            <Field label="Maximum file size">
              <Select value={input.maxBytes} onChange={(e) => set('maxBytes', Number(e.target.value))}>
                {[1, 2, 4, 8].map((m) => <option key={m} value={m * 1024 * 1024}>{m} MiB</option>)}
              </Select>
            </Field>
            <Field label="Bytes per packet" hint="Longer packets include more payload.">
              <Select value={input.snapLength} onChange={(e) => set('snapLength', Number(e.target.value))}>
                {[128, 512, 4096].map((n) => <option key={n} value={n}>{n} bytes</option>)}
              </Select>
            </Field>
          </fieldset>
          <div className="mt-4 flex flex-wrap items-center justify-between gap-3 border-t border-line pt-3">
            <p className="max-w-xl text-[12px] leading-5 text-muted">Packets can contain private traffic or credentials. One capture runs at a time and stops at the first limit. Capture only traffic you intend to inspect.</p>
            <Button type="submit" variant="primary" write disabled={busy || active || !available || !status?.supported}>
              {busy ? <Spinner size={13} /> : <Play size={13} />} {active ? 'Capture running' : 'Start capture'}
            </Button>
          </div>
        </form>
      </Panel>
      <Panel title="Recent captures" actions={<Button size="sm" disabled={busy} onClick={() => void load()}><RefreshCw size={12} /> Refresh</Button>}>
        <p className="mb-3 text-[11px] leading-4 text-faint">The AP keeps up to three captures in temporary storage for ten minutes after they finish. Restarting the manager or AP clears them. Download a PCAP to open it in Wireshark.</p>
        <div className="space-y-2" aria-live="polite">
          {!status?.jobs.length && <p className="py-3 text-[12px] text-muted">No captures retained.</p>}
          {status?.jobs.map((job) => <CaptureRow key={job.id} job={job} busy={busy} action={action} />)}
        </div>
      </Panel>
    </div>
  )
}

function CaptureRow({ job, busy, action }: { job: CaptureJob; busy: boolean; action: (run: () => Promise<unknown>) => Promise<void> }) {
  const v = job.input
  return <div className="flex flex-wrap items-center justify-between gap-3 rounded border border-line p-3">
    <div className="min-w-0">
      <div className="flex flex-wrap items-center gap-2"><span className="mono text-[13px] text-ink">{v.interface}</span><Badge tone={job.state === 'running' ? 'accent' : job.state === 'failed' ? 'warn' : 'ok'}>{job.state === 'running' ? 'Running' : job.state === 'stopped' ? 'Stopped' : job.state === 'failed' ? 'Failed' : 'Completed'}</Badge><span className="tabular text-[11px] text-faint">{new Date(job.startedAt).toLocaleTimeString()}</span></div>
      <p className="mt-1 break-words text-[12px] text-muted">{v.protocol === 'all' ? 'All protocols' : v.protocol.toUpperCase()}{v.host ? ` · ${v.host}` : ''}{v.port ? ` · port ${v.port}` : ''} · {v.seconds} s · {v.snapLength} bytes/packet</p>
      <p className="tabular mt-1 text-[11px] text-faint">{job.packets} packets · {size(job.bytes)} / {size(v.maxBytes)}{job.reason ? ` · ${reasons[job.reason] || job.reason}` : ''}{job.expiresAt ? ` · expires ${new Date(job.expiresAt).toLocaleTimeString()}` : ''}</p>
      {job.error && <p role="alert" className="mt-1 text-[12px] text-danger">{job.error}</p>}
    </div>
    <div className="flex gap-2">
      {job.state === 'running' ? <Button write size="sm" disabled={busy} onClick={() => void action(() => api.stopCapture(job.id))}><Square size={12} /> Stop</Button> : <>
        <Button write size="sm" disabled={busy || !job.download} onClick={() => void action(() => api.downloadCapture(job.id))}><Download size={12} /> Download</Button>
        <Button write size="sm" disabled={busy} onClick={() => void action(() => api.deleteCapture(job.id))}><Trash2 size={12} /> Delete</Button>
      </>}
    </div>
  </div>
}

export function SupportBundlePanel() {
  const toast = useApp((s) => s.toast)
  const [clients, setClients] = useState(false)
  const [events, setEvents] = useState(true)
  const [busy, setBusy] = useState(false)
  const download = async () => {
    setBusy(true)
    try { await api.supportBundle(clients, events); toast('Support bundle downloaded.', 'ok') } catch (e) { toast(e instanceof Error ? e.message : String(e), 'danger') } finally { setBusy(false) }
  }
  return <Panel title="Download support bundle">
    <p className="mb-4 text-[13px] leading-5 text-muted">A ZIP with device and radio status, wireless interfaces, management settings and network paths. Each snapshot records its collection time, with warnings for missing or stale data.</p>
    <div className="max-w-lg space-y-2">
      <Toggle label="Include client details" hint="Includes client MAC/IP addresses, hostnames and learned network neighbours. Account usernames are omitted." checked={clients} onChange={setClients} disabled={busy} />
      <Toggle label="Include recent wireless events" hint="Up to 150 parsed connection and radio events. Client MAC addresses follow the client-details switch." checked={events} onChange={setEvents} disabled={busy} />
    </div>
    <p className="mt-4 text-[12px] leading-5 text-muted">Passwords, keys, tokens, SNMP communities, raw logs, raw configuration and packet files are excluded. Network names and AP identifiers remain in the bundle; review it before sharing.</p>
    <div className="mt-4 flex justify-end border-t border-line pt-3"><Button variant="primary" write={false} disabled={busy} onClick={() => void download()}>{busy ? <Spinner size={13} /> : <Download size={13} />} {busy ? 'Preparing bundle…' : 'Download support bundle'}</Button></div>
  </Panel>
}

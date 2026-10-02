import { useEffect, useState } from 'react'
import { Activity, Play, RefreshCw } from 'lucide-react'
import { Page } from '@/app/Page'
import { api } from '@/api'
import { useApp } from '@/stores/app'
import type { DiagnosticResult, WirelessStatus } from '@/types'
import { Badge, Button, EmptyState, Field, Input, Panel, Select, Spinner } from '@/ui/kit'

export function DiagnosticsPage() {
  return (
    <Page title="Diagnostics" description="Test connectivity from the AP and inspect its live wireless interfaces.">
      <div className="space-y-3">
        <ConnectivityPanel />
        <WirelessStatusPanel />
      </div>
    </Page>
  )
}
function ConnectivityPanel() {
  const gateway = useApp((s) => s.state?.device.gateway)
  const [tool, setTool] = useState('ping')
  const [target, setTarget] = useState('')
  const [port, setPort] = useState('80')
  const [busy, setBusy] = useState(false)
  const [result, setResult] = useState<DiagnosticResult | null>(null)
  const [error, setError] = useState('')
  const run = async () => {
    setBusy(true)
    setError('')
    setResult(null)
    try {
      setResult(await api.diagnose(tool, target.trim(), tool === 'tcp' ? Number(port) : undefined))
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }
  return (
    <Panel title="Network tests">
      <form
        className="grid items-end gap-3 sm:grid-cols-[160px_1fr_auto]"
        onSubmit={(e) => {
          e.preventDefault()
          if (!busy && (tool !== 'tcp' || (Number.isInteger(Number(port)) && Number(port) >= 1 && Number(port) <= 65535))) void run()
        }}
      >
        <Field label="Test">
          <Select disabled={busy} value={tool} onChange={(e) => setTool(e.target.value)}>
            <option value="ping">Ping</option>
            <option value="dns">DNS lookup</option>
            <option value="trace">Trace route</option>
            <option value="tcp">TCP port</option>
          </Select>
        </Field>
        <div className="flex items-end gap-2">
          <Field label="Target" className="min-w-0 flex-1">
            <Input
              required
              maxLength={253}
              autoCapitalize="none"
              spellCheck={false}
              placeholder={tool === 'dns' ? 'aboutus-net.intern' : gateway || 'Hostname or IP address'}
              disabled={busy}
              value={target}
              onChange={(e) => setTarget(e.target.value)}
            />
          </Field>
          {tool === 'tcp' && (
            <Field label="Port" className="w-24">
              <Input type="number" min={1} max={65535} required disabled={busy} value={port} onChange={(e) => setPort(e.target.value)} />
            </Field>
          )}
        </div>
        <Button type="submit" variant="primary" disabled={busy || !target.trim()}>
          {busy ? <Spinner size={13} /> : <Play size={13} />} {busy ? 'Testing…' : 'Run test'}
        </Button>
      </form>
      <div className="mt-2 flex flex-wrap items-center gap-2 text-[11px] text-faint">
        <span>Runs from the AP’s management network. {tool === 'tcp' ? 'Connects within 5 seconds.' : 'Stops within 15 seconds.'}</span>
        {gateway && (
          <button
            type="button"
            className="text-accent hover:underline disabled:opacity-40"
            disabled={busy}
            onClick={() => {
              setTool('ping')
              setTarget(gateway)
            }}
          >
            Use gateway
          </button>
        )}
      </div>
      {error && (
        <p role="alert" className="mt-3 text-[12px] text-danger">
          {error}
        </p>
      )}
      {result && (
        <div className="mt-4 overflow-hidden rounded border border-line">
          <div className="flex items-center gap-2 border-b border-line bg-surface-2 px-3 py-2">
            <Badge tone={result.success ? 'ok' : 'warn'}>{result.timedOut ? 'Timed out' : result.success ? 'Completed' : 'Failed'}</Badge>
            <span className="mono min-w-0 flex-1 truncate text-xs text-muted">
              {result.target}
              {result.port ? ` · port ${result.port}` : ''}
            </span>
            <span className="tabular text-[11px] text-faint">{(result.durationMs / 1000).toFixed(1)} s</span>
          </div>
          <pre aria-live="polite" className="mono max-h-80 overflow-auto whitespace-pre-wrap break-all p-3 text-[12px] leading-5 text-muted">
            {result.output || 'No output returned.'}
          </pre>
        </div>
      )}
    </Panel>
  )
}
function WirelessStatusPanel() {
  const [rows, setRows] = useState<WirelessStatus[]>([])
  const [sampledAt, setSampledAt] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const load = async () => {
    setBusy(true)
    setError('')
    try {
      const data = await api.wirelessStatus()
      setRows(data.interfaces)
      setSampledAt(data.sampledAt)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }
  useEffect(() => {
    void load()
  }, [])
  return (
    <Panel
      title="Live wireless interfaces"
      bodyClassName="p-0"
      actions={
        <Button size="sm" disabled={busy} onClick={() => void load()}>
          {busy ? <Spinner size={12} /> : <RefreshCw size={12} />} Refresh
        </Button>
      }
    >
      <div className="border-b border-line px-3 py-2 text-[11px] leading-4 text-faint">
        Actual hostapd state, beacon interval (TU) and DTIM interval (beacons). {sampledAt && `Sampled ${new Date(sampledAt).toLocaleTimeString()}.`}
      </div>
      {error && (
        <p role="alert" className="px-3 py-3 text-[12px] text-danger">
          {error}
        </p>
      )}
      {!rows.length ? (
        <EmptyState
          icon={<Activity size={25} />}
          title={busy ? 'Reading wireless interfaces…' : 'No wireless interfaces'}
          description={busy ? 'Waiting for the AP.' : 'Interfaces appear when the radios finish starting.'}
        />
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full min-w-[850px] text-left text-[12px]">
            <thead className="border-b border-line text-2xs font-semibold uppercase tracking-wider text-faint">
              <tr>
                {['Interface / BSSID', 'Network', 'State', 'Frequency', 'Clients', 'Beacon / DTIM', 'Wi-Fi'].map((h) => (
                  <th key={h} className="px-3 py-2">
                    {h}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody className="divide-y divide-line">
              {rows.map((row) => {
                const v = row.values
                return (
                  <tr key={row.interface}>
                    <td className="px-3 py-2">
                      <p className="mono text-ink">{row.interface}</p>
                      <p className="mono text-[11px] text-faint">{v['bssid[0]'] || '—'}</p>
                    </td>
                    <td className="px-3 py-2 text-ink">{v['ssid[0]'] || '—'}</td>
                    <td className="px-3 py-2">
                      <Badge tone={v.state === 'ENABLED' ? 'ok' : 'warn'}>{row.error || v.state || 'Unknown'}</Badge>
                      {v.state === 'DFS' && <p className="mt-1 text-faint">CAC: {v.cac_time_left_seconds} s</p>}
                    </td>
                    <td className="tabular px-3 py-2 text-muted">{v.freq ? `${v.freq} MHz` : '—'}</td>
                    <td className="tabular px-3 py-2 text-muted">{v['num_sta[0]'] ?? '—'}</td>
                    <td className="tabular px-3 py-2 text-muted">
                      {v.beacon_int ?? '—'} / {v.dtim_period ?? '—'}
                    </td>
                    <td className="px-3 py-2 text-muted">
                      {[
                        ['ieee80211n', '4'],
                        ['ieee80211ac', '5'],
                        ['ieee80211ax', '6'],
                        ['ieee80211be', '7'],
                      ]
                        .filter(([key]) => key && v[key] === '1')
                        .map(([, label]) => label)
                        .join(' / ') || '—'}
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      )}
    </Panel>
  )
}

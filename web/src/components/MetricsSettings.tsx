import { useEffect, useState } from 'react'
import { Copy, Eye, EyeOff, KeyRound } from 'lucide-react'
import { api } from '@/api'
import { useApp } from '@/stores/app'
import type { MetricsSettings } from '@/types'
import { Badge, Button, IconButton, KeyValue, Panel, SubSection, Spinner } from '@/ui/kit'

function copy(text: string, toast: (t: string) => void) {
  void navigator.clipboard?.writeText(text).then(
    () => toast('Copied.'),
    () => toast('Copying is not allowed here; select the text instead.'),
  )
}

/** Prometheus scrape endpoint, protected by a bearer token. */
/** With `bare`, renders as a section of a shared Monitoring panel. */
export function MetricsPanel({ bare = false }: { bare?: boolean }) {
  const Box = bare ? SubSection : Panel
  const toast = useApp((s) => s.toast)
  const ip = useApp((s) => s.state?.device.mgmtIp)
  const isAdmin = useApp((s) => s.role === 'admin')
  const [m, setM] = useState<MetricsSettings | null>(null)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [reveal, setReveal] = useState(false)

  useEffect(() => {
    api
      .metrics()
      .then(setM)
      .catch((e: unknown) => setError(e instanceof Error ? e.message : String(e)))
  }, [])

  const update = async (enabled: boolean, newToken: boolean) => {
    setBusy(true)
    try {
      setM(await api.updateMetrics(enabled, newToken))
      toast(newToken ? 'New token created. Update it in Prometheus.' : enabled ? 'Metrics endpoint enabled.' : 'Metrics endpoint disabled.', 'ok')
    } catch (e) {
      toast(e instanceof Error ? e.message : String(e), 'danger')
    } finally {
      setBusy(false)
    }
  }

  const url = `http://${ip || '<ap-address>'}/metrics`
  const scrape = m
    ? `- job_name: c460\n  metrics_path: /metrics\n  authorization:\n    credentials: ${reveal ? m.token : '<token>'}\n  static_configs:\n    - targets: ['${ip || '<ap-address>'}:80']`
    : ''

  return (
    <Box
      title={bare ? 'Prometheus' : 'Monitoring · Prometheus'}
      help="Exposes clients, radios, Ethernet counters, temperature and health findings in Prometheus format at /metrics. Scrapers must send the token as a bearer token. Off by default."
      actions={
        m && (
          <Button size="sm" write disabled={busy} onClick={() => void update(!m.enabled, false)}>
            {busy && <Spinner size={12} />}
            {m.enabled ? 'Turn off' : 'Turn on'}
          </Button>
        )
      }
    >
      {error ? (
        <p role="alert" className="text-[12px] text-danger">
          {error}
        </p>
      ) : !m ? (
        <p className="text-[12px] text-muted">Reading metrics settings…</p>
      ) : (
        <>
          <KeyValue items={[{ label: 'Status', value: <Badge tone={m.enabled ? 'ok' : 'neutral'}>{m.enabled ? 'On' : 'Off'}</Badge> }]} />
          {m.enabled && (
            <>
              <KeyValue
                className="mt-1.5"
                items={[
                  { label: 'Endpoint', value: url, mono: true },
                  {
                    label: 'Token',
                    value: isAdmin ? (
                      <span className="flex min-w-0 items-center gap-1">
                        <span className="mono truncate">{reveal ? m.token : '••••••••••••'}</span>
                        <IconButton label={reveal ? 'Hide token' : 'Show token'} size="sm" onClick={() => setReveal((v) => !v)}>
                          {reveal ? <EyeOff size={12} /> : <Eye size={12} />}
                        </IconButton>
                        <IconButton label="Copy token" size="sm" onClick={() => copy(m.token, toast)}>
                          <Copy size={12} />
                        </IconButton>
                      </span>
                    ) : (
                      <span className="text-faint">Only administrators can see it</span>
                    ),
                  },
                ]}
              />
              {isAdmin && (
                <>
                  <p className="mb-1 mt-3 text-2xs font-semibold uppercase tracking-wider text-faint">prometheus.yml</p>
                  <pre className="mono overflow-x-auto rounded border border-line bg-surface-2 p-2 text-[11px] leading-4 text-ink">{scrape}</pre>
                  <div className="mt-2 flex justify-end">
                    <Button size="sm" write disabled={busy} onClick={() => void update(true, true)} title="Old scrapers stop working until they get the new token">
                      <KeyRound size={12} /> New token
                    </Button>
                  </div>
                </>
              )}
            </>
          )}
        </>
      )}
    </Box>
  )
}

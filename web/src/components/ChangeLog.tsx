import { useEffect, useMemo, useState } from 'react'
import { Download, RefreshCw, Search } from 'lucide-react'
import { api } from '@/api'
import type { ChangeEntry } from '@/types'
import { cn } from '@/ui/cn'
import { Button, EmptyState, Input, Panel, Spinner } from '@/ui/kit'

const PAGE = 50

/** Who changed what and when; the AP keeps the newest entries up to a fixed limit. */
export function ChangeLogPanel() {
  const [entries, setEntries] = useState<ChangeEntry[] | null>(null)
  const [limit, setLimit] = useState(300)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)
  const [query, setQuery] = useState('')
  const [shown, setShown] = useState(PAGE)

  const load = async () => {
    setLoading(true)
    try {
      const r = await api.changes()
      setEntries(r.entries)
      setLimit(r.limit)
      setError('')
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setLoading(false)
    }
  }
  useEffect(() => {
    void load()
  }, [])

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase()
    return (entries ?? []).filter((e) => !q || `${e.action} ${e.user} ${e.address} ${e.error ?? ''}`.toLowerCase().includes(q))
  }, [entries, query])

  const exportText = () => {
    const text = filtered.map((e) => `${e.time}\t${e.user}\t${e.address}\t${e.ok ? 'ok' : 'failed'}\t${e.action}${e.error ? ` (${e.error})` : ''}`).join('\n')
    const url = URL.createObjectURL(new Blob([text + '\n'], { type: 'text/plain' }))
    const a = document.createElement('a')
    a.href = url
    a.download = `c460-changes-${new Date().toISOString().slice(0, 10)}.txt`
    a.click()
    setTimeout(() => URL.revokeObjectURL(url), 1000)
  }

  return (
    <Panel
      title="Configuration changes"
      help={`Every change made through this web interface or by a schedule, newest first. The AP keeps the latest ${limit} entries, so the log never grows without limit.`}
      bodyClassName="p-0"
      actions={
        <>
          <Button size="sm" write={false} disabled={!filtered.length} onClick={exportText} aria-label="Export as text">
            <Download size={12} />
          </Button>
          <Button size="sm" disabled={loading} onClick={() => void load()} aria-label="Refresh change log">
            {loading ? <Spinner size={12} /> : <RefreshCw size={12} />}
          </Button>
        </>
      }
    >
      <div className="border-b border-line p-2">
        <div className="relative max-w-2xl">
          <Search size={13} className="pointer-events-none absolute left-2 top-2 text-faint" />
          <Input value={query} onChange={(e) => setQuery(e.target.value)} placeholder="Search changes, users or addresses" className="h-7 pl-7" aria-label="Search changes" />
        </div>
      </div>
      {error ? (
        <p role="alert" className="p-3 text-[12px] text-danger">
          {error}
        </p>
      ) : !entries ? (
        <p className="p-3 text-[12px] text-muted">Loading…</p>
      ) : filtered.length === 0 ? (
        <EmptyState title={query ? 'No matching changes' : 'No changes yet'} description={query ? 'Try a different search.' : 'Changes made in this interface appear here.'} />
      ) : (
        <>
          <ul className="divide-y divide-line">
            {filtered.slice(0, shown).map((e, i) => (
              <li key={`${e.time}-${i}`} className="flex gap-3 px-3 py-2 text-[13px]">
                <span className={cn('mt-1.5 h-2 w-2 shrink-0 rounded-full', e.ok ? 'bg-ok' : 'bg-danger')} title={e.ok ? 'Applied' : 'Failed'} />
                <span className="min-w-0 flex-1">
                  <span className="block break-words text-ink">{e.action}</span>
                  {e.error && <span className="block text-[12px] text-danger">Failed: {e.error}</span>}
                  <span className="block text-[11px] text-faint">
                    {e.user}
                    {e.address && ` · ${e.address}`}
                  </span>
                </span>
                <time className="tabular shrink-0 text-right text-[11px] leading-4 text-muted" dateTime={e.time} title={new Date(e.time).toString()}>
                  {new Date(e.time).toLocaleDateString([], { month: 'short', day: 'numeric' })}
                  <br />
                  {new Date(e.time).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}
                </time>
              </li>
            ))}
          </ul>
          {filtered.length > shown && (
            <div className="border-t border-line p-2 text-center">
              <Button size="sm" variant="ghost" onClick={() => setShown((n) => n + PAGE)}>
                Show {Math.min(PAGE, filtered.length - shown)} more of {filtered.length - shown}
              </Button>
            </div>
          )}
        </>
      )}
    </Panel>
  )
}

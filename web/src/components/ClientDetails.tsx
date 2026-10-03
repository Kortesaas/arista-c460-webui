import { useNavigate } from 'react-router-dom'
import { useEffect, useState } from 'react'
import { RefreshCw } from 'lucide-react'
import { api } from '@/api'
import { useApp } from '@/stores/app'
import type { Client, NativeClient } from '@/types'
import { Badge, Button, Dialog, DialogActions, KeyValue, SectionLabel, Spinner } from '@/ui/kit'
import { ClientSignal } from '@/components/ClientSignal'
import { formatBytes, formatDuration } from '@/utils/format'

export function ClientDialog({ client, onClose }: { client: Client; onClose: () => void }) {
  const navigate = useNavigate()
  const [native, setNative] = useState<NativeClient | null>(null)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [confirm, setConfirm] = useState(false)
  const toast = useApp((s) => s.toast)
  const refresh = useApp((s) => s.refresh)
  const sampledAt = useApp((s) => s.state?.generatedAt)
  useEffect(() => {
    let active = true
    api
      .clientDetails(client.mac)
      .then((v) => {
        if (active) setNative(v)
      })
      .catch((e) => {
        if (active) setError(e.message)
      })
    return () => {
      active = false
    }
  }, [client.mac])
  const reconnect = async () => {
    setBusy(true)
    try {
      await api.reconnectClient(client.mac)
      toast('Client disconnected. It can reconnect immediately.', 'ok')
      void refresh()
      onClose()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
      setConfirm(false)
    } finally {
      setBusy(false)
    }
  }
  const v = native?.values ?? {}
  return (
    <Dialog
      title={client.hostname || client.ipv4 || 'Client details'}
      description={`${client.mac} · ${client.ssid}`}
      wide
      onClose={() => {
        if (!busy) onClose()
      }}
    >
      <div className="grid gap-4 sm:grid-cols-2">
        <KeyValue
          items={[
            { label: 'IPv4', value: client.ipv4 || '—', mono: true },
            { label: 'Address source', value: !client.ipv4 ? 'Unknown' : client.ipv4Source === 'arp' ? 'AP ARP cache' : 'AP telemetry' },
            { label: 'IPv6', value: client.ipv6.join(', ') || '—', mono: true },
            { label: 'Band', value: client.band ? `${client.band} GHz` : '—' },
            { label: 'VLAN', value: client.vlan ?? 'Native / untagged' },
            { label: 'Signal', value: client.rssi === null ? '—' : `${client.rssi} dBm` },
            { label: 'Spatial streams', value: client.streams && client.streams > 0 ? client.streams : '—' },
          ]}
        />
        <KeyValue
          items={[
            { label: 'Interface', value: native?.interface || '—', mono: true },
            { label: 'Connected', value: v.connected_time ? formatDuration(Number(v.connected_time)) : '—' },
            { label: 'Inactive', value: v.inactive_msec ? `${v.inactive_msec} ms` : '—' },
            { label: 'Received', value: v.rx_bytes ? formatBytes(Number(v.rx_bytes)) : formatBytes(client.rxBytes) },
            { label: 'Sent', value: v.tx_bytes ? formatBytes(Number(v.tx_bytes)) : formatBytes(client.txBytes) },
            { label: 'Association ID', value: v.aid || '—' },
          ]}
        />
      </div>
      <div className="mt-4 border-t border-line pt-3">
        <SectionLabel className="mb-2">Signal, last 2 hours</SectionLabel>
        <ClientSignal mac={client.mac} sampledAt={sampledAt} />
      </div>
      {v.flags && (
        <div className="mt-4 flex flex-wrap gap-1">
          {v.flags.match(/\[[^\]]+\]/g)?.map((flag) => (
            <Badge key={flag}>{flag.slice(1, -1)}</Badge>
          ))}
        </div>
      )}
      {error ? (
        <p role="alert" className="mt-3 text-[12px] text-danger">
          {error}
        </p>
      ) : !native ? (
        <p className="mt-3 text-[12px] text-muted">Reading live client details…</p>
      ) : null}
      {confirm && (
        <div className="mt-4 rounded border border-warn bg-warn-soft p-3 text-[12px] leading-5 text-warn">
          Disconnect this client briefly so it can negotiate a new connection? Active calls or transfers may be interrupted. If this is your device, the WebUI may disconnect too.
        </div>
      )}
      <DialogActions>
        {!confirm && client.ipv4 && (
          <Button
            disabled={busy}
            onClick={() => {
              onClose()
              navigate(`/diagnostics?target=${encodeURIComponent(client.ipv4)}`)
            }}
          >
            Test connection
          </Button>
        )}
        <Button disabled={busy} onClick={() => (confirm ? setConfirm(false) : onClose())}>
          {confirm ? 'Cancel' : 'Close'}
        </Button>
        <Button write variant={confirm ? 'danger' : 'default'} disabled={busy || !native || Boolean(error)} onClick={() => (confirm ? void reconnect() : setConfirm(true))}>
          {busy ? <Spinner size={13} /> : <RefreshCw size={13} />}
          {confirm ? 'Disconnect client' : 'Reconnect client'}
        </Button>
      </DialogActions>
    </Dialog>
  )
}

import { useEffect, useMemo, useState } from 'react'
import { Eye, EyeOff, Printer } from 'lucide-react'
import qrcode from 'qrcode-generator'
import { api } from '@/api'
import type { JoinCode } from '@/types'
import { Button, Dialog, DialogActions, IconButton, Spinner } from '@/ui/kit'
import { opModeShort } from '@/utils/format'

// Wi-Fi names and passwords may contain any Unicode character; the library
// only keeps the low byte of each character, so encode as UTF-8 instead.
qrcode.stringToBytes = (s: string) => Array.from(new TextEncoder().encode(s))

function QrSvg({ text, size = 232 }: { text: string; size?: number }) {
  const cells = useMemo(() => {
    const qr = qrcode(0, 'M')
    qr.addData(text)
    qr.make()
    const n = qr.getModuleCount()
    const dark: [number, number][] = []
    for (let r = 0; r < n; r++) for (let c = 0; c < n; c++) if (qr.isDark(r, c)) dark.push([c, r])
    return { n, dark }
  }, [text])
  const margin = 4
  const total = cells.n + margin * 2
  return (
    <svg viewBox={`0 0 ${total} ${total}`} width={size} height={size} shapeRendering="crispEdges" role="img" aria-label="QR code to join the network" className="rounded bg-white">
      <rect width={total} height={total} fill="#fff" />
      {cells.dark.map(([x, y]) => (
        <rect key={`${x}-${y}`} x={x + margin} y={y + margin} width={1} height={1} fill="#000" />
      ))}
    </svg>
  )
}

/** Opens a print view of the join card in a new window, with nothing else on the page. */
function printCard(code: JoinCode, svg: string) {
  const win = window.open('', '_blank', 'noopener=no,width=480,height=640')
  if (!win) return
  const esc = (s: string) => s.replace(/[&<>"]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' })[c]!)
  win.document.write(`<!doctype html><html><head><meta charset="utf-8"><title>${esc(code.ssid)}</title>
<style>body{font-family:system-ui,sans-serif;text-align:center;margin:40px;color:#111}h1{font-size:28px;margin:0 0 6px}p{margin:4px 0;font-size:15px}.pw{font-family:ui-monospace,monospace;font-size:18px;margin-top:10px}svg{width:280px;height:280px;margin:24px auto}</style>
</head><body><h1>${esc(code.ssid)}</h1><p>Scan with the phone camera to join</p>${svg}
${code.password ? `<p>Password</p><p class="pw">${esc(code.password)}</p>` : '<p>No password needed</p>'}
<script>window.onload=()=>{window.print()}</script></body></html>`)
  win.document.close()
}

export function JoinCodeDialog({ name, onClose }: { name: string; onClose: () => void }) {
  const [code, setCode] = useState<JoinCode | null>(null)
  const [error, setError] = useState('')
  const [reveal, setReveal] = useState(false)

  useEffect(() => {
    api
      .joinCode(name)
      .then(setCode)
      .catch((e: unknown) => setError(e instanceof Error ? e.message : String(e)))
  }, [name])

  return (
    <Dialog title={`Join “${name}”`} description="Point a phone camera at the code to connect without typing the password." onClose={onClose}>
      {error ? (
        <p role="alert" className="text-[13px] text-danger">
          {error}
        </p>
      ) : !code ? (
        <p className="flex items-center gap-2 text-[13px] text-muted">
          <Spinner size={13} /> Preparing the code…
        </p>
      ) : (
        <div className="flex flex-col items-center gap-3">
          <div id="join-qr">
            <QrSvg text={code.payload} />
          </div>
          <div className="w-full rounded border border-line bg-surface-2 px-3 py-2 text-[13px]">
            <div className="flex items-center justify-between gap-2">
              <span className="text-muted">Security</span>
              <span className="text-ink">{opModeShort(code.opmode)}</span>
            </div>
            {code.password && (
              <div className="mt-1 flex items-center justify-between gap-2">
                <span className="text-muted">Password</span>
                <span className="flex min-w-0 items-center gap-1">
                  <span className="mono truncate text-ink">{reveal ? code.password : '••••••••••'}</span>
                  <IconButton label={reveal ? 'Hide password' : 'Show password'} size="sm" onClick={() => setReveal((v) => !v)}>
                    {reveal ? <EyeOff size={13} /> : <Eye size={13} />}
                  </IconButton>
                </span>
              </div>
            )}
            {code.hidden && <p className="mt-1 text-[12px] text-faint">The network name is hidden; the code includes it, so phones still find it.</p>}
          </div>
        </div>
      )}
      <DialogActions>
        <Button onClick={onClose}>Close</Button>
        <Button
          write={false}
          disabled={!code}
          onClick={() => {
            const svg = document.querySelector('#join-qr svg')?.outerHTML
            if (code && svg) printCard(code, svg)
          }}
        >
          <Printer size={13} /> Print card
        </Button>
      </DialogActions>
    </Dialog>
  )
}

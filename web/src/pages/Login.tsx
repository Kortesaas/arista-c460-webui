import { useState, type FormEvent } from 'react'
import { LogIn } from 'lucide-react'
import { useApp } from '@/stores/app'
import { LogoMark } from '@/ui/Logo'
import { Button, Field, Input, Spinner } from '@/ui/kit'

export function LoginPage() {
  const { signIn, configured } = useApp()
  const [password, setPassword] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    setBusy(true)
    setError(null)
    try {
      await signIn(password)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
      setPassword('')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="grid h-full place-items-center p-4">
      <div className="w-full max-w-sm">
        <div className="mb-5 flex items-center gap-3">
          <span className="grid h-10 w-10 place-items-center rounded-lg bg-ink text-[var(--surface)]">
            <LogoMark size={24} />
          </span>
          <div>
            <p className="text-[11px] font-bold uppercase tracking-[0.14em] text-ink">C-460</p>
            <p className="text-[13px] text-muted">Access point management</p>
          </div>
        </div>
        <form onSubmit={(event) => void submit(event)} className="rounded-lg border border-line bg-surface p-4 shadow-card">
          <h1 className="mb-1 text-base font-semibold text-ink">Sign in</h1>
          <p className="mb-4 text-[12px] leading-5 text-muted">
            {configured ? 'Enter the administrator password of this access point.' : 'No password has been set on this access point yet. Set one during installation (see README).'}
          </p>
          <Field label="Password">
            <Input type="password" autoComplete="current-password" autoFocus value={password} onChange={(event) => setPassword(event.target.value)} disabled={!configured || busy} />
          </Field>
          {error && <p className="mt-2 text-[12px] text-danger">{error}</p>}
          <Button type="submit" variant="primary" className="mt-4 w-full" disabled={!configured || busy || password.length === 0}>
            {busy ? <Spinner size={13} /> : <LogIn size={14} />}
            Sign in
          </Button>
        </form>
        <p className="mt-4 text-center text-[11px] text-faint">Unofficial local web interface · not affiliated with Arista Networks</p>
      </div>
    </div>
  )
}

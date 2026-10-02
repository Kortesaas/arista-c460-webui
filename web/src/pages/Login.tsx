import { useState, type FormEvent } from 'react'
import { LogIn } from 'lucide-react'
import { useApp } from '@/stores/app'
import { Brand } from '@/ui/Brand'
import { Button, Field, Input, Spinner } from '@/ui/kit'

export function LoginPage() {
  const { signIn, configured } = useApp()
  const [username, setUsername] = useState(() => {
    try {
      return localStorage.getItem('c460-username') ?? ''
    } catch {
      return ''
    }
  })
  const [password, setPassword] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    setBusy(true)
    setError(null)
    try {
      await signIn(username.trim(), password)
      try {
        localStorage.setItem('c460-username', username.trim())
      } catch {
        /* private mode */
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
      setPassword('')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="flex h-full flex-col">
      <div className="h-1.5 shrink-0 bg-brand-bar" />
      <div className="grid flex-1 place-items-center overflow-auto p-4">
      <div className="w-full max-w-sm">
        <div className="mb-6 flex flex-col items-center text-brand">
          <Brand height={100} />
          <p className="mt-3 text-[12px] font-medium uppercase tracking-[0.18em] text-muted">Access point management</p>
        </div>
        <form onSubmit={(event) => void submit(event)} className="rounded-lg border border-line bg-surface p-4 shadow-card">
          <h1 className="mb-1 text-base font-semibold text-ink">Sign in</h1>
          <p className="mb-4 text-[12px] leading-5 text-muted">
            {configured ? 'Use the administrator account of this access point.' : 'No administrator account has been set up on this access point yet. Set one during installation (see README).'}
          </p>
          <Field label="Username" className="mb-3">
            <Input autoComplete="username" autoCapitalize="none" spellCheck={false} autoFocus={!username} value={username} onChange={(event) => setUsername(event.target.value)} placeholder="config" disabled={!configured || busy} />
          </Field>
          <Field label="Password">
            <Input type="password" autoComplete="current-password" autoFocus={Boolean(username)} value={password} onChange={(event) => setPassword(event.target.value)} disabled={!configured || busy} />
          </Field>
          {error && <p className="mt-2 text-[12px] text-danger">{error}</p>}
          <Button type="submit" variant="primary" className="mt-4 w-full" disabled={!configured || busy || password.length === 0 || username.trim().length === 0}>
            {busy ? <Spinner size={13} /> : <LogIn size={14} />}
            Sign in
          </Button>
        </form>
        <p className="mt-4 text-center text-[11px] leading-4 text-faint">
          Unofficial interface. Not affiliated with Arista Networks.
        </p>
      </div>
      </div>
    </div>
  )
}

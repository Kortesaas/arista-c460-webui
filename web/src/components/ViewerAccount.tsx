import { useState } from 'react'
import { Eye, Trash2, UserPlus } from 'lucide-react'
import { api } from '@/api'
import { useApp } from '@/stores/app'
import { Button, Dialog, DialogActions, Field, Input, KeyValue, Panel, Spinner } from '@/ui/kit'

/** A second login for read-only access to AP status. */
export function ViewerPanel() {
  const { viewer, setViewer, toast, role } = useApp()
  const [editing, setEditing] = useState(false)
  const [removing, setRemoving] = useState(false)
  const [busy, setBusy] = useState(false)
  if (role !== 'admin') return null

  const remove = async () => {
    setBusy(true)
    try {
      await api.removeViewer()
      setViewer('')
      toast('Read-only account removed; its sessions were signed out.', 'ok')
      setRemoving(false)
    } catch (e) {
      toast(e instanceof Error ? e.message : String(e), 'danger')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Panel
      title="Read-only account"
      help="A second login for people who should see the AP's status but not change it. It can view every page and run connection tests; it cannot save settings, see passwords or tokens, or restart the AP."
      actions={
        viewer ? (
          <Button size="sm" variant="ghost" onClick={() => setRemoving(true)} aria-label="Remove read-only account">
            <Trash2 size={12} />
          </Button>
        ) : undefined
      }
    >
      {viewer ? (
        <>
          <KeyValue items={[{ label: 'Username', value: viewer, mono: true }, { label: 'Access', value: 'View only' }]} />
          <div className="mt-3 flex justify-end">
            <Button size="sm" onClick={() => setEditing(true)}>
              Change login
            </Button>
          </div>
        </>
      ) : (
        <div className="flex items-center justify-between gap-3">
          <p className="flex items-center gap-2 text-[12px] text-muted">
            <Eye size={14} className="shrink-0 text-faint" /> No read-only account yet.
          </p>
          <Button size="sm" onClick={() => setEditing(true)}>
            <UserPlus size={12} /> Create
          </Button>
        </div>
      )}
      {editing && <ViewerDialog current={viewer} onClose={() => setEditing(false)} />}
      {removing && (
        <Dialog title="Remove the read-only account?" description={`“${viewer}” is signed out everywhere and can no longer log in.`} onClose={() => !busy && setRemoving(false)}>
          <DialogActions>
            <Button disabled={busy} onClick={() => setRemoving(false)}>
              Cancel
            </Button>
            <Button variant="danger" disabled={busy} onClick={() => void remove()}>
              {busy ? <Spinner size={12} /> : <Trash2 size={13} />} Remove
            </Button>
          </DialogActions>
        </Dialog>
      )}
    </Panel>
  )
}

function ViewerDialog({ current, onClose }: { current: string; onClose: () => void }) {
  const { setViewer, toast, username: admin } = useApp()
  const [username, setUsername] = useState(current || 'viewer')
  const [password, setPassword] = useState('')
  const [confirm, setConfirm] = useState('')
  const [busy, setBusy] = useState(false)
  const name = username.trim()
  const problem = !/^[A-Za-z0-9._-]{1,32}$/.test(name)
    ? "Use 1–32 letters, digits, '.', '_' or '-'."
    : name.toLowerCase() === admin.toLowerCase()
      ? 'The administrator already uses this name.'
      : password.length < 6
        ? 'The password needs at least 6 characters.'
        : password !== confirm
          ? 'The passwords do not match.'
          : ''
  const save = async () => {
    setBusy(true)
    try {
      const r = await api.setViewer(name, password)
      setViewer(r.viewer)
      toast(current ? 'Read-only login changed; old sessions were signed out.' : `Read-only account “${r.viewer}” created.`, 'ok')
      onClose()
    } catch (e) {
      toast(e instanceof Error ? e.message : String(e), 'danger')
    } finally {
      setBusy(false)
    }
  }
  return (
    <Dialog title={current ? 'Change read-only login' : 'Create read-only account'} description="Share this login with people who should only look." onClose={() => !busy && onClose()}>
      <form
        className="space-y-3"
        onSubmit={(e) => {
          e.preventDefault()
          if (!problem && !busy) void save()
        }}
      >
        <Field label="Username">
          <Input value={username} autoComplete="off" autoCapitalize="none" spellCheck={false} maxLength={32} onChange={(e) => setUsername(e.target.value)} />
        </Field>
        <Field label="Password">
          <Input type="password" autoComplete="new-password" value={password} onChange={(e) => setPassword(e.target.value)} />
        </Field>
        <Field label="Repeat password">
          <Input type="password" autoComplete="new-password" value={confirm} onChange={(e) => setConfirm(e.target.value)} />
        </Field>
        {(password || confirm) && problem && <p className="text-[12px] text-warn">{problem}</p>}
        <DialogActions>
          <Button disabled={busy} onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" variant="primary" disabled={busy || Boolean(problem)}>
            {busy && <Spinner size={12} />} {current ? 'Save' : 'Create account'}
          </Button>
        </DialogActions>
      </form>
    </Dialog>
  )
}

import { useState } from 'react'
import { Layers, Trash2, X } from 'lucide-react'
import { useApp } from '@/stores/app'
import { changeLabel, useStaging } from '@/stores/staging'
import { Button, Dialog, DialogActions, IconButton, Spinner } from '@/ui/kit'

/** Sticky bar at the bottom of the page while changes are waiting to be applied. */
export function StagedBar() {
  const { changes, busy, applyAll, discard } = useStaging()
  const [review, setReview] = useState(false)
  if (!changes.length) return null
  return (
    <>
      <div className="shrink-0 border-t border-accent bg-accent-soft px-3 py-2 sm:px-4 lg:px-6" role="region" aria-label="Pending changes">
        <div className="mx-auto flex max-w-[1500px] flex-wrap items-center gap-2">
          <Layers size={15} className="shrink-0 text-accent-text" />
          <p className="min-w-0 flex-1 text-[13px] text-ink">
            <span className="font-semibold">
              {changes.length} pending {changes.length === 1 ? 'change' : 'changes'}
            </span>
            <span className="hidden text-muted sm:inline"> · applied together, so Wi-Fi restarts only once</span>
          </p>
          <Button size="sm" onClick={() => setReview(true)}>
            Review
          </Button>
          <Button size="sm" variant="ghost" disabled={busy} onClick={discard}>
            Discard
          </Button>
          <Button size="sm" variant="primary" disabled={busy} onClick={() => void applyAll()}>
            {busy && <Spinner size={12} />} Apply all
          </Button>
        </div>
      </div>
      {review && <ReviewDialog onClose={() => setReview(false)} />}
    </>
  )
}

function ReviewDialog({ onClose }: { onClose: () => void }) {
  const { changes, busy, remove, discard, applyAll } = useStaging()
  const radios = useApp((s) => s.state?.radios) ?? []
  const bandOf = (id: number) => radios.find((r) => r.id === id)?.band ?? String(id)
  return (
    <Dialog title="Pending changes" description="These changes are sent to the AP together. Wi-Fi restarts once for a few seconds." onClose={() => !busy && onClose()}>
      {changes.length === 0 ? (
        <p className="text-[13px] text-muted">Nothing pending.</p>
      ) : (
        <ol className="divide-y divide-line rounded border border-line">
          {changes.map((c, i) => (
            <li key={i} className="flex items-center gap-2 px-3 py-2 text-[13px]">
              <span className="tabular w-5 shrink-0 text-faint">{i + 1}.</span>
              <span className="min-w-0 flex-1 text-ink">{changeLabel(c, bandOf)}</span>
              <IconButton label="Remove this change" size="md" onClick={() => remove(i)}>
                <X size={14} />
              </IconButton>
            </li>
          ))}
        </ol>
      )}
      <DialogActions>
        <Button
          variant="ghost"
          disabled={busy || !changes.length}
          onClick={() => {
            discard()
            onClose()
          }}
        >
          <Trash2 size={13} /> Discard all
        </Button>
        <Button
          variant="primary"
          disabled={busy || !changes.length}
          onClick={() => {
            onClose()
            void applyAll()
          }}
        >
          {busy && <Spinner size={12} />} Apply {changes.length} {changes.length === 1 ? 'change' : 'changes'}
        </Button>
      </DialogActions>
    </Dialog>
  )
}

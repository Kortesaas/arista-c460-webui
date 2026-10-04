import type { ReactNode } from 'react'
import type { FeatureSettings, FeatureValue } from '@/types'
import { cn } from '@/ui/cn'
import { HelpTip, Input, Segmented } from '@/ui/kit'

export interface FeatureDef {
  key: string
  label: string
  help: ReactNode
  /** Integer setting with a range instead of on/off. */
  range?: [number, number]
  unit?: string
}

export interface FeatureGroup {
  title: string
  items: FeatureDef[]
}

type Choice = 'default' | 'on' | 'off'
const toChoice = (v: FeatureValue): Choice => (v === null || v === undefined ? 'default' : v ? 'on' : 'off')
const fromChoice = (c: Choice): FeatureValue => (c === 'default' ? null : c === 'on')

function Running({ value, unit }: { value: boolean | number | undefined; unit?: string }) {
  if (value === undefined) return null
  const text = typeof value === 'number' ? `${value}${unit ? ` ${unit}` : ''}` : value ? 'on' : 'off'
  return (
    <span className={cn('tabular text-[11px]', value === true ? 'text-ok' : 'text-faint')} title="What the firmware currently runs">
      now {text}
    </span>
  )
}

/**
 * Grouped advanced settings. Every setting can stay at the firmware default;
 * the current running value is shown next to each so "default" is never a guess.
 */
export function FeatureEditor({
  groups,
  values,
  native,
  onChange,
  disabled,
}: {
  groups: FeatureGroup[]
  values: FeatureSettings
  native: Record<string, boolean | number>
  onChange: (key: string, value: FeatureValue) => void
  disabled?: boolean
}) {
  return (
    <fieldset disabled={disabled} className="space-y-4">
      {groups.map((group) => (
        <section key={group.title}>
          <h3 className="mb-1.5 text-2xs font-semibold uppercase tracking-wider text-faint">{group.title}</h3>
          <div className="divide-y divide-line rounded border border-line">
            {group.items.map((f) => (
              <div key={f.key} className="flex flex-wrap items-center gap-x-3 gap-y-1.5 px-3 py-2">
                <div className="flex min-w-0 flex-1 basis-[12rem] items-center gap-1.5">
                  <span className="text-[13px] text-ink">{f.label}</span>
                  <HelpTip label={f.label}>{f.help}</HelpTip>
                </div>
                <Running value={native[f.key]} unit={f.unit} />
                {f.range ? (
                  <div className="flex items-center gap-1.5">
                    <Segmented
                      value={values[f.key] === null || values[f.key] === undefined ? 'default' : 'custom'}
                      onChange={(c) => onChange(f.key, c === 'default' ? null : typeof native[f.key] === 'number' ? (native[f.key] as number) : f.range![0])}
                      options={[
                        { value: 'default', label: 'Default' },
                        { value: 'custom', label: 'Set' },
                      ]}
                    />
                    <Input
                      type="number"
                      min={f.range[0]}
                      max={f.range[1]}
                      aria-label={f.label}
                      disabled={values[f.key] === null || values[f.key] === undefined}
                      value={typeof values[f.key] === 'number' ? String(values[f.key]) : ''}
                      onChange={(e) => onChange(f.key, e.target.value === '' ? f.range![0] : Number(e.target.value))}
                      className="h-7 w-16"
                    />
                    {f.unit && <span className="text-[12px] text-muted">{f.unit}</span>}
                  </div>
                ) : (
                  <Segmented
                    value={toChoice(values[f.key] ?? null)}
                    onChange={(c) => onChange(f.key, fromChoice(c))}
                    options={[
                      { value: 'default', label: 'Default' },
                      { value: 'on', label: 'On' },
                      { value: 'off', label: 'Off' },
                    ]}
                    className="w-[13rem]"
                  />
                )}
              </div>
            ))}
          </div>
        </section>
      ))}
    </fieldset>
  )
}

/** Only the settings that differ from what was loaded. */
export function changedSettings(original: FeatureSettings, edited: FeatureSettings): FeatureSettings {
  const out: FeatureSettings = {}
  for (const [k, v] of Object.entries(edited)) if ((original[k] ?? null) !== (v ?? null)) out[k] = v ?? null
  return out
}

export function rangeProblems(groups: FeatureGroup[], values: FeatureSettings): string[] {
  const out: string[] = []
  for (const f of groups.flatMap((g) => g.items)) {
    const v = values[f.key]
    if (f.range && typeof v === 'number' && (!Number.isInteger(v) || v < f.range[0] || v > f.range[1])) out.push(`${f.label}: ${f.range[0]}–${f.range[1]}${f.unit ? ` ${f.unit}` : ''}.`)
  }
  return out
}

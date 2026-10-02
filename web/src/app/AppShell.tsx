import { useEffect, useState, type ReactNode } from 'react'
import { NavLink, useLocation } from 'react-router-dom'
import { Antenna, Activity, Gauge, LogOut, Menu, Monitor, Moon, PanelLeft, Radar, RefreshCw, Server, Sun, Users, Wifi } from 'lucide-react'
import { cn } from '@/ui/cn'
import { Brand } from '@/ui/Brand'
import { Badge } from '@/ui/kit'
import { useApp, type Connection } from '@/stores/app'
import { useThemeStore, type Theme } from '@/stores/theme'
import { Age } from '@/components/status'

const nav = [
  { label: 'Overview', to: '/overview', icon: Gauge },
  { label: 'Wireless networks', to: '/wireless', icon: Wifi },
  { label: 'Radios', to: '/radios', icon: Antenna },
  { label: 'Clients', to: '/clients', icon: Users, badge: 'clients' as const },
  { label: 'RF scan', to: '/scan', icon: Radar },
  { label: 'Diagnostics', to: '/diagnostics', icon: Activity },
  { label: 'System', to: '/system', icon: Server },
]

const titles: [string, string][] = [
  ['/overview', 'Overview'],
  ['/wireless', 'Wireless networks'],
  ['/radios', 'Radios'],
  ['/clients', 'Clients'],
  ['/scan', 'RF scan'],
  ['/diagnostics', 'Diagnostics'],
  ['/system', 'System'],
]

function ThemeToggle() {
  const { theme, setTheme } = useThemeStore()
  const options: { value: Theme; icon: typeof Sun; label: string }[] = [
    { value: 'light', icon: Sun, label: 'Light theme' },
    { value: 'dark', icon: Moon, label: 'Dark theme' },
    { value: 'system', icon: Monitor, label: 'System theme' },
  ]
  return (
    <div className="flex h-7 items-center gap-0.5 rounded border border-line bg-surface-2 px-0.5">
      {options.map((option) => (
        <button
          key={option.value}
          type="button"
          onClick={() => setTheme(option.value)}
          aria-label={option.label}
          aria-pressed={theme === option.value}
          title={option.label}
          className={cn('grid h-6 w-6 place-items-center rounded-sm transition-colors', theme === option.value ? 'bg-surface text-ink shadow-card' : 'text-faint hover:text-ink')}
        >
          <option.icon size={13} />
        </button>
      ))}
    </div>
  )
}

const connectionLabel: Record<Connection, { label: string; dot: string }> = {
  connecting: { label: 'Connecting…', dot: 'bg-faint' },
  live: { label: 'Live', dot: 'bg-ok pulse-ok' },
  reconnecting: { label: 'Reconnecting…', dot: 'bg-warn' },
  offline: { label: 'Unreachable', dot: 'bg-danger' },
}

function LiveStatus({ collapsed }: { collapsed: boolean }) {
  const { state, connection, error } = useApp()
  const item = connectionLabel[connection]
  const apError = Boolean(state?.error)
  if (collapsed)
    return (
      <div className="flex justify-center py-1" title={item.label}>
        <span className={cn('h-2 w-2 rounded-full', apError ? 'bg-warn' : item.dot)} />
      </div>
    )
  return (
    <div className="space-y-1 px-1 text-[11px] leading-4">
      <div className="flex items-center gap-1.5 text-ink">
        <span className={cn('h-2 w-2 shrink-0 rounded-full', apError ? 'bg-warn' : item.dot)} />
        <span className="font-medium">{apError ? 'Agent not answering' : item.label}</span>
        {state && (
          <span className="ml-auto text-faint">
            <Age iso={state.generatedAt} />
          </span>
        )}
      </div>
      <p className="truncate text-faint" title={error ?? undefined}>
        {error ?? `Updates every ${state?.pollSeconds ?? 5}s`}
      </p>
    </div>
  )
}

export function AppShell({ children }: { children: ReactNode }) {
  const [collapsed, setCollapsed] = useState(() => {
    try {
      return localStorage.getItem('c460-sidebar') === 'collapsed'
    } catch {
      return false
    }
  })
  const [mobileOpen, setMobileOpen] = useState(false)
  const [refreshing, setRefreshing] = useState(false)
  const location = useLocation()
  const { state, connection, refresh, signOut } = useApp()

  useEffect(() => {
    try {
      localStorage.setItem('c460-sidebar', collapsed ? 'collapsed' : 'open')
    } catch {
      /* ignore */
    }
  }, [collapsed])

  const title = titles.find(([prefix]) => location.pathname.startsWith(prefix))?.[1] ?? 'Access point'
  const device = state?.device

  useEffect(() => {
    document.title = device ? `${title} · ${device.siteName || device.hostname} · ARRR-ISTA` : 'ARRR-ISTA C460'
  }, [title, device])
  const clients = state?.clients.length ?? 0

  return (
    <div className="flex h-full flex-col overflow-hidden">
      <header className="flex h-14 shrink-0 items-center gap-3 border-b border-brand-bar-line bg-brand-bar px-3 text-white sm:px-4">
        <button type="button" aria-label="Open navigation" onClick={() => setMobileOpen(true)} className="grid h-8 w-8 place-items-center rounded text-white/80 hover:bg-white/10 hover:text-white lg:hidden">
          <Menu size={17} />
        </button>
        <NavLink to="/overview" className="flex items-center" aria-label="Overview">
          <Brand height={38} className="text-white" />
        </NavLink>
        <span className="hidden h-6 w-px bg-white/20 sm:block" />
        <div className="hidden min-w-0 flex-col leading-tight sm:flex">
          <span className="truncate text-[13px] font-semibold">{device?.siteName || device?.hostname || 'Access point'}</span>
          <span className="mono truncate text-[11px] text-white/60">{device ? `${device.model} · ${device.mgmtIp} · ${device.firmware}` : ''}</span>
        </div>

        <div className="ml-auto flex items-center gap-1.5">
          {connection !== 'live' && <Badge tone={connection === 'offline' ? 'danger' : 'warn'}>{connectionLabel[connection].label}</Badge>}
          <button
            type="button"
            onClick={() => {
              setRefreshing(true)
              void refresh().finally(() => setRefreshing(false))
            }}
            className="inline-flex h-8 items-center gap-1.5 rounded border border-white/20 px-2.5 text-[12px] font-medium text-white/85 transition-colors hover:bg-white/10 hover:text-white"
          >
            <RefreshCw size={13} className={cn(refreshing && 'spin')} />
            <span className="hidden sm:inline">Refresh</span>
          </button>
          <button type="button" onClick={() => void signOut()} aria-label="Sign out" title="Sign out" className="grid h-8 w-8 place-items-center rounded text-white/80 hover:bg-white/10 hover:text-white">
            <LogOut size={15} />
          </button>
        </div>
      </header>

      <div className="flex min-h-0 flex-1 overflow-hidden">
      {mobileOpen && <button aria-label="Close navigation" onClick={() => setMobileOpen(false)} className="fixed inset-0 z-40 bg-black/50 lg:hidden" />}
      <aside
        className={cn(
          'fixed inset-y-0 left-0 z-50 flex shrink-0 flex-col border-r border-line bg-surface transition-[width,transform] duration-150 lg:static lg:translate-x-0',
          collapsed ? 'w-[56px]' : 'w-[224px]',
          mobileOpen ? 'translate-x-0' : '-translate-x-full',
        )}
      >
        <div className={cn('flex h-10 shrink-0 items-center gap-2 border-b border-line px-3 text-brand', collapsed && 'justify-center px-0')}>
          {collapsed ? (
            <Brand variant="hat" height={18} />
          ) : (
            <span className="truncate text-2xs font-semibold uppercase tracking-wider text-faint">{device?.model ?? 'C-460'} access point</span>
          )}
        </div>

        <nav className="min-h-0 flex-1 overflow-y-auto px-2 py-2" aria-label="Main">
          {nav.map((item) => (
            <NavLink
              key={item.to}
              to={item.to}
              onClick={() => setMobileOpen(false)}
              title={collapsed ? item.label : undefined}
              className={({ isActive }) =>
                cn(
                  'mb-0.5 flex h-8 items-center gap-2.5 rounded px-2 text-[13px] font-medium transition-colors',
                  isActive ? 'bg-accent-soft text-accent-text' : 'text-muted hover:bg-surface-2 hover:text-ink',
                  collapsed && 'justify-center px-0',
                )
              }
            >
              <item.icon size={15} className="shrink-0" />
              {!collapsed && <span className="truncate">{item.label}</span>}
              {!collapsed && item.badge === 'clients' && clients > 0 && <span className="tabular ml-auto rounded-sm bg-surface-2 px-1.5 text-2xs font-bold text-muted">{clients}</span>}
            </NavLink>
          ))}
        </nav>

        <div className="shrink-0 space-y-2 border-t border-line p-2">
          <LiveStatus collapsed={collapsed} />
          {!collapsed && (
            <div className="flex items-center justify-between px-1">
              <span className="text-[11px] text-faint">Theme</span>
              <ThemeToggle />
            </div>
          )}
          <button
            type="button"
            onClick={() => setCollapsed((value) => !value)}
            aria-label={collapsed ? 'Expand sidebar' : 'Collapse sidebar'}
            className="hidden h-7 w-full items-center justify-center gap-2 rounded text-[12px] text-faint hover:bg-surface-2 hover:text-ink lg:flex"
          >
            <PanelLeft size={14} />
            {!collapsed && 'Collapse'}
          </button>
        </div>
      </aside>

      <div className="flex min-w-0 flex-1 flex-col">
        {state?.error && (
          <div className="shrink-0 border-b border-line bg-warn-soft px-3 py-1.5 text-[12px] leading-4 text-warn">{state.error} — showing the last known values.</div>
        )}
        <main className="min-h-0 flex-1 overflow-auto">{children}</main>
      </div>
      </div>
    </div>
  )
}

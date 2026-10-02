import { useEffect } from 'react'
import { Navigate, Route, Routes } from 'react-router-dom'
import { AppShell } from '@/app/AppShell'
import { Toasts } from '@/components/Toasts'
import { PirateLoading } from '@/components/Loading'
import { useApp } from '@/stores/app'
import { LoginPage } from '@/pages/Login'
import { OverviewPage } from '@/pages/Overview'
import { WirelessPage } from '@/pages/Wireless'
import { RadiosPage } from '@/pages/Radios'
import { ClientsPage } from '@/pages/Clients'
import { ScanPage } from '@/pages/Scan'
import { SystemPage } from '@/pages/System'

export function App() {
  const init = useApp((store) => store.init)
  const auth = useApp((store) => store.auth)
  useEffect(() => init(), [init])

  if (auth === 'unknown') return <PirateLoading />

  return (
    <>
      {auth === 'signed-out' ? (
        <LoginPage />
      ) : (
        <AppShell>
          <Routes>
            <Route path="/" element={<Navigate to="/overview" replace />} />
            <Route path="/overview" element={<OverviewPage />} />
            <Route path="/wireless" element={<WirelessPage />} />
            <Route path="/radios" element={<RadiosPage />} />
            <Route path="/clients" element={<ClientsPage />} />
            <Route path="/scan" element={<ScanPage />} />
            <Route path="/system" element={<SystemPage />} />
            <Route path="*" element={<Navigate to="/overview" replace />} />
          </Routes>
        </AppShell>
      )}
      <Toasts />
    </>
  )
}

import { Navigate, Route, Routes } from 'react-router-dom'
import AdminLayout from './layouts/AdminLayout'
import Login from './pages/Login'
import Dashboard from './pages/Dashboard'
import { useAuth } from './stores/auth'

// Business pages
import Farms from './pages/Farms'
import Plots from './pages/Plots'
import DeviceMap from './pages/DeviceMap'
import Tasks from './pages/Tasks'
import Missions from './pages/Missions'
import TraceEvents from './pages/TraceEvents'
import TraceCodes from './pages/TraceCodes'
import Packages from './pages/Packages'
import AiAssistant from './pages/AiAssistant'
import Users from './pages/Users'
import Roles from './pages/Roles'
import AuditLogs from './pages/AuditLogs'
import { SimpleRoutes } from './pages/SimpleRoutes'
import PlatformTenants from './pages/PlatformTenants'
import TenantInfo from './pages/TenantInfo'
import RemoteSensing from './pages/RemoteSensing'
import WeatherPage from './pages/WeatherPage' 

/** Route registry: simple resources are generated inline from CrudPage. */
export default function App() {
  const user = useAuth((s) => s.user)
  if (!user) {
    return <Routes><Route path="*" element={<Login />} /></Routes>
  }
  return (
    <Routes>
      <Route element={<AdminLayout />}>
        <Route path="/" element={<Dashboard />} />
        <Route path="/login" element={<Navigate to="/" replace />} />

        <Route path="/farms" element={<Farms />} />
        <Route path="/plots" element={<Plots />} />
        <Route path="/device-map" element={<DeviceMap />} />
        <Route path="/agricultural-tasks" element={<Tasks />} />
        <Route path="/missions" element={<Missions />} />
        <Route path="/trace-events" element={<TraceEvents />} />
        <Route path="/trace-codes" element={<TraceCodes />} />
        <Route path="/packages" element={<Packages />} />
        <Route path="/ai" element={<AiAssistant />} />
        <Route path="/users" element={<Users />} />
        <Route path="/roles" element={<Roles />} />
        <Route path="/audit-logs" element={<AuditLogs />} />
        <Route path="/tenant" element={<TenantInfo />} />
        <Route path="/remote-sensing" element={<RemoteSensing />} />
        <Route path="/weather" element={<WeatherPage />} />
        {user.platform_admin && <Route path="/platform/tenants" element={<PlatformTenants />} />}

        {/* Simple resources rendered by the shared CrudPage */}
        <Route path="*" element={<SimpleRoutes />} />
      </Route>
    </Routes>
  )
}

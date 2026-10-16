import { Navigate, useLocation } from 'react-router-dom'
import type { ReactNode } from 'react'
import { useAuth } from '../auth/AuthContext'
import type { Role } from '../api/types'

export default function RequireAuth({ roles, children }: { roles?: Role[]; children: ReactNode }) {
  const { user } = useAuth()
  const loc = useLocation()
  if (!user) return <Navigate to="/entrar" replace state={{ from: loc.pathname }} />
  if (roles && !roles.includes(user.role)) {
    return (
      <div className="page narrow">
        <h1>Sin acceso</h1>
        <p>Tu cuenta no tiene permisos para ver esta sección.</p>
      </div>
    )
  }
  return <>{children}</>
}

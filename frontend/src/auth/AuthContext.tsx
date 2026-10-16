import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'
import { api, getToken, setToken, setUnauthorizedHandler } from '../api/client'
import type { User } from '../api/types'

const USER_KEY = 'mooc.user'

interface AuthState {
  user: User | null
  login(email: string, password: string): Promise<User>
  register(fullName: string, email: string, password: string): Promise<void>
  logout(): Promise<void>
}

const Ctx = createContext<AuthState | null>(null)

function readUser(): User | null {
  try {
    const raw = localStorage.getItem(USER_KEY)
    return raw && getToken() ? (JSON.parse(raw) as User) : null
  } catch { return null }
}

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<User | null>(readUser)

  const clear = useCallback(() => {
    setToken(null)
    try { localStorage.removeItem(USER_KEY) } catch { /* sin almacenamiento */ }
    setUser(null)
  }, [])

  useEffect(() => {
    setUnauthorizedHandler(clear)
    return () => setUnauthorizedHandler(null)
  }, [clear])

  const login = useCallback(async (email: string, password: string) => {
    const out = await api<{ user: User; session: { token: string } }>('/auth/login', {
      method: 'POST', body: { email, password },
    })
    setToken(out.session.token)
    try { localStorage.setItem(USER_KEY, JSON.stringify(out.user)) } catch { /* sin almacenamiento */ }
    setUser(out.user)
    return out.user
  }, [])

  const register = useCallback(async (fullName: string, email: string, password: string) => {
    const out = await api<{ verification_code: string }>('/auth/register', {
      method: 'POST', body: { full_name: fullName, email, password },
    })
    // La plataforma aún no envía correos: el servidor devuelve el código y la
    // verificación se completa aquí para que la cuenta quede lista para entrar.
    await api('/auth/verify-email', { method: 'POST', body: { token: out.verification_code } })
  }, [])

  const logout = useCallback(async () => {
    try { await api('/auth/logout', { method: 'POST' }) } catch { /* la sesión local se limpia igual */ }
    clear()
  }, [clear])

  const value = useMemo(() => ({ user, login, register, logout }), [user, login, register, logout])
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>
}

export function useAuth(): AuthState {
  const v = useContext(Ctx)
  if (!v) throw new Error('useAuth requiere AuthProvider')
  return v
}

// Cliente HTTP mínimo para el envelope {success, data, error, details}.
const TOKEN_KEY = 'mooc.token'

export class ApiError extends Error {
  status: number
  details: string[]
  constructor(message: string, status: number, details: string[] = []) {
    super(message)
    this.status = status
    this.details = details
  }
}

export function getToken(): string | null {
  try { return localStorage.getItem(TOKEN_KEY) } catch { return null }
}
export function setToken(token: string | null) {
  try { token ? localStorage.setItem(TOKEN_KEY, token) : localStorage.removeItem(TOKEN_KEY) } catch { /* sin almacenamiento */ }
}

let onUnauthorized: (() => void) | null = null
export function setUnauthorizedHandler(fn: (() => void) | null) { onUnauthorized = fn }

export async function api<T>(path: string, init: { method?: string; body?: unknown } = {}): Promise<T> {
  const headers: Record<string, string> = { Accept: 'application/json' }
  const token = getToken()
  if (token) headers.Authorization = `Bearer ${token}`
  if (init.body !== undefined) headers['Content-Type'] = 'application/json'

  let res: Response
  try {
    res = await fetch(`/api/v1${path}`, {
      method: init.method ?? 'GET',
      headers,
      body: init.body !== undefined ? JSON.stringify(init.body) : undefined,
    })
  } catch {
    throw new ApiError('No hay conexión con el servidor. Revisa tu red e inténtalo de nuevo.', 0)
  }

  let payload: { success?: boolean; data?: T; error?: string; details?: string[] } = {}
  try { payload = await res.json() } catch { /* cuerpo vacío */ }

  if (res.status === 401 && token) onUnauthorized?.()
  if (!res.ok || payload.success === false) {
    throw new ApiError(payload.error || `Error ${res.status} del servidor`, res.status, payload.details ?? [])
  }
  return payload.data as T
}

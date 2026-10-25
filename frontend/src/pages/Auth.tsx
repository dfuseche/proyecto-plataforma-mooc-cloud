import { useState, type FormEvent } from 'react'
import { Link, useLocation, useNavigate } from 'react-router-dom'
import { useAuth } from '../auth/AuthContext'
import { ErrorNote, Field } from '../components/ui'

export function Login() {
  const { login } = useAuth()
  const navigate = useNavigate()
  const loc = useLocation()
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const from = (loc.state as { from?: string } | null)?.from
  const registered = (loc.state as { registered?: boolean } | null)?.registered

  async function submit(e: FormEvent) {
    e.preventDefault()
    setBusy(true); setError(null)
    try {
      const u = await login(email.trim(), password)
      navigate(from ?? (u.role === 'student' ? '/mis-cursos' : '/docente'), { replace: true })
    } catch (err) {
      setError(err instanceof Error ? err.message : 'No se pudo iniciar sesión.')
    } finally { setBusy(false) }
  }

  return (
    <div className="page narrow">
      <h1>Entrar a tu cuenta</h1>
      {registered && <div className="note note-ok" role="status"><p>Cuenta creada. Ya puedes entrar.</p></div>}
      <form onSubmit={submit} className="form">
        <Field label="Correo electrónico" name="email" type="email" value={email} onChange={setEmail} autoComplete="email" />
        <Field label="Contraseña" name="password" type="password" value={password} onChange={setPassword} autoComplete="current-password" />
        {error && <ErrorNote message={error} />}
        <button type="submit" className="btn" disabled={busy}>{busy ? 'Entrando…' : 'Entrar'}</button>
      </form>
      <p>¿Aún no tienes cuenta? <Link to="/registro">Crea una</Link>.</p>
    </div>
  )
}

export function Register() {
  const { register } = useAuth()
  const navigate = useNavigate()
  const [fullName, setFullName] = useState('')
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  async function submit(e: FormEvent) {
    e.preventDefault()
    setBusy(true); setError(null)
    try {
      await register(fullName.trim(), email.trim(), password)
      navigate('/entrar', { replace: true, state: { registered: true } })
    } catch (err) {
      setError(err instanceof Error ? err.message : 'No se pudo crear la cuenta.')
    } finally { setBusy(false) }
  }

  return (
    <div className="page narrow">
      <h1>Crear cuenta de estudiante</h1>
      <form onSubmit={submit} className="form">
        <Field label="Nombre completo" name="name" value={fullName} onChange={setFullName} autoComplete="name" />
        <Field label="Correo electrónico" name="email" type="email" value={email} onChange={setEmail} autoComplete="email" />
        <Field label="Contraseña" name="password" type="password" value={password} onChange={setPassword}
          autoComplete="new-password" minLength={8} hint="Mínimo 8 caracteres." />
        {error && <ErrorNote message={error} />}
        <button type="submit" className="btn" disabled={busy}>{busy ? 'Creando…' : 'Crear cuenta'}</button>
      </form>
      <p>¿Ya tienes cuenta? <Link to="/entrar">Entra aquí</Link>.</p>
    </div>
  )
}

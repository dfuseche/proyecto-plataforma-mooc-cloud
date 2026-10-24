import { useState, type FormEvent } from 'react'
import { api } from '../../api/client'
import type { User } from '../../api/types'
import { ErrorNote, Field } from '../../components/ui'

export default function AdminTeachers() {
  const [fullName, setFullName] = useState('')
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [created, setCreated] = useState<User[]>([])

  async function submit(e: FormEvent) {
    e.preventDefault()
    setBusy(true); setError(null)
    try {
      const teacher = await api<User>('/admin/teachers', {
        method: 'POST', body: { full_name: fullName.trim(), email: email.trim(), password },
      })
      setCreated((prev) => [teacher, ...prev])
      setFullName(''); setEmail(''); setPassword('')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'No se pudo crear la cuenta de docente.')
    } finally { setBusy(false) }
  }

  return (
    <div className="page narrow">
      <h1>Cuentas de docentes</h1>
      <p>Crea la cuenta de una persona que va a publicar cursos. Entrega la contraseña temporal por un canal seguro.</p>
      <form onSubmit={submit} className="form">
        <Field label="Nombre completo" name="teacher-name" value={fullName} onChange={setFullName} autoComplete="off" />
        <Field label="Correo electrónico" name="teacher-email" type="email" value={email} onChange={setEmail} autoComplete="off" />
        <Field label="Contraseña temporal" name="teacher-password" type="password" value={password} onChange={setPassword}
          autoComplete="new-password" minLength={8} hint="Mínimo 8 caracteres." />
        {error && <ErrorNote message={error} />}
        <button className="btn" disabled={busy}>{busy ? 'Creando…' : 'Crear docente'}</button>
      </form>
      {created.length > 0 && (
        <section aria-live="polite">
          <h2 className="section-title">Creadas en esta sesión</h2>
          <ul>
            {created.map((t) => <li key={t.id}>{t.full_name} ({t.email})</li>)}
          </ul>
        </section>
      )}
    </div>
  )
}

import { useState, type FormEvent } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { api } from '../../api/client'
import type { Course, CourseVersion } from '../../api/types'
import { useAsync } from '../../api/useAsync'
import { useAuth } from '../../auth/AuthContext'
import { Empty, ErrorNote, Field, Loading } from '../../components/ui'

export default function TeacherDashboard() {
  const { user } = useAuth()
  const navigate = useNavigate()
  const courses = useAsync(() => api<Course[]>('/courses?limit=200'), [])
  const [title, setTitle] = useState('')
  const [summary, setSummary] = useState('')
  const [passing, setPassing] = useState('70')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const mine = (courses.data ?? []).filter((c) => user?.role === 'admin' || c.created_by_teacher_id === user?.id)

  async function create(e: FormEvent) {
    e.preventDefault()
    setBusy(true); setError(null)
    try {
      const out = await api<{ course: Course; version: CourseVersion }>('/courses', {
        method: 'POST', body: { title: title.trim(), summary: summary.trim(), passing_score: Number(passing) },
      })
      navigate(`/docente/cursos/${out.course.id}`)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'No se pudo crear el curso.')
    } finally { setBusy(false) }
  }

  return (
    <div className="page">
      <h1>Docencia</h1>
      <div className="split">
        <section>
          <h2 className="section-title">Tus cursos</h2>
          {courses.loading && <Loading />}
          {courses.error && <ErrorNote message={courses.error} onRetry={courses.reload} />}
          {courses.data && mine.length === 0 && <Empty title="Aún no creas cursos"><p>Crea el primero con el formulario.</p></Empty>}
          <ul className="rows">
            {mine.map((c) => (
              <li key={c.id}>
                <Link to={`/docente/cursos/${c.id}`}>
                  <strong>{c.title}</strong>
                  <span className={`tag ${c.current_published_version_id ? 'tag-ok' : ''}`}>{c.current_published_version_id ? 'Publicado' : 'Borrador'}</span>
                </Link>
              </li>
            ))}
          </ul>
        </section>
        <section>
          <h2 className="section-title">Nuevo curso</h2>
          <form onSubmit={create} className="form">
            <Field label="Título" name="title" value={title} onChange={setTitle} />
            <div className="field">
              <label htmlFor="f-summary">Descripción</label>
              <textarea id="f-summary" value={summary} onChange={(e) => setSummary(e.target.value)} rows={3} required />
            </div>
            <Field label="Puntaje mínimo para aprobar (0 a 100)" name="passing" type="number" value={passing} onChange={setPassing} />
            {error && <ErrorNote message={error} />}
            <button className="btn" disabled={busy}>{busy ? 'Creando…' : 'Crear curso'}</button>
          </form>
        </section>
      </div>
    </div>
  )
}

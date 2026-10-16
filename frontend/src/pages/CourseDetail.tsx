import { useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { api } from '../api/client'
import type { Course, CourseVersion, Enrollment } from '../api/types'
import { useAsync } from '../api/useAsync'
import { useAuth } from '../auth/AuthContext'
import { ErrorNote, Loading, Progress } from '../components/ui'

export default function CourseDetail() {
  const { courseId = '' } = useParams()
  const { user } = useAuth()
  const navigate = useNavigate()
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const course = useAsync(async () => {
    const list = await api<Course[]>('/courses?published=true&limit=100')
    return list.find((c) => c.id === courseId) ?? null
  }, [courseId])
  const tree = useAsync(() => api<CourseVersion>(`/courses/${courseId}`), [courseId])
  const mine = useAsync(
    () => (user ? api<Enrollment[]>('/learning/enrollments') : Promise.resolve([] as Enrollment[])),
    [user?.id],
  )

  const enrollment = mine.data?.find((e) => e.course_id === courseId)
  const modules = tree.data?.modules ?? []
  const firstResource = modules.flatMap((m) => m.units ?? []).flatMap((u) => u.resources ?? []).find((r) => r.is_visible)

  async function enroll() {
    if (!user) { navigate('/entrar', { state: { from: `/cursos/${courseId}` } }); return }
    setBusy(true); setError(null)
    try {
      await api('/learning/enrollments', { method: 'POST', body: { course_id: courseId } })
      navigate(`/aprender/${courseId}`)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'No se pudo completar la inscripción.')
    } finally { setBusy(false) }
  }

  if (course.loading || tree.loading) return <div className="page"><Loading /></div>
  if (tree.error || !course.data) return <div className="page"><ErrorNote message={tree.error ?? 'No encontramos este curso o aún no está publicado.'} /><p><Link to="/">Volver a los cursos</Link></p></div>

  return (
    <div className="page">
      <p className="crumb"><Link to="/">Cursos</Link></p>
      <h1>{course.data.title}</h1>
      <p className="lead">{course.data.summary}</p>
      <div className="actions">
        {enrollment ? (
          <>
            <Link to={`/aprender/${courseId}`} className="btn">{enrollment.progress_percentage > 0 ? 'Continuar curso' : 'Empezar curso'}</Link>
            <div className="inline-progress"><Progress value={enrollment.progress_percentage} label="Avance del curso" /><span>{Math.round(enrollment.progress_percentage)}%</span></div>
          </>
        ) : (
          <button className="btn" onClick={enroll} disabled={busy || !firstResource}>{busy ? 'Inscribiendo…' : 'Inscribirme gratis'}</button>
        )}
      </div>
      {error && <ErrorNote message={error} />}
      <h2 className="section-title">Contenido</h2>
      <ol className="outline">
        {modules.map((m) => (
          <li key={m.id}>
            <h3>{m.title}</h3>
            {m.description && <p>{m.description}</p>}
            <ul>
              {m.units?.map((u) => (
                <li key={u.id}>{u.title}<span className="muted"> · {(u.resources ?? []).filter((r) => r.is_visible).length} recursos</span></li>
              ))}
            </ul>
          </li>
        ))}
      </ol>
    </div>
  )
}

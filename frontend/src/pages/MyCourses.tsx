import { Link } from 'react-router-dom'
import { api } from '../api/client'
import type { Enrollment } from '../api/types'
import { useAsync } from '../api/useAsync'
import { Empty, ErrorNote, Loading, Progress } from '../components/ui'

export default function MyCourses() {
  const mine = useAsync(() => api<Enrollment[]>('/learning/enrollments'), [])
  return (
    <div className="page">
      <h1>Mis cursos</h1>
      {mine.loading && <Loading />}
      {mine.error && <ErrorNote message={mine.error} onRetry={mine.reload} />}
      {mine.data && mine.data.length === 0 && (
        <Empty title="Aún no estás inscrito en ningún curso">
          <p><Link to="/">Explora el catálogo</Link> y empieza con el primero.</p>
        </Empty>
      )}
      <ul className="cards">
        {mine.data?.map((e) => (
          <li key={e.id}>
            <Link to={`/aprender/${e.course_id}`} className="card">
              <h3>{e.course_title}</h3>
              <p>{e.course_summary}</p>
              <Progress value={e.progress_percentage} label={`Avance en ${e.course_title}`} />
              <span className="card-cta">
                {e.academic_status === 'approved' ? 'Aprobado' : e.progress_percentage > 0 ? `Continuar · ${Math.round(e.progress_percentage)}%` : 'Empezar'}
              </span>
            </Link>
          </li>
        ))}
      </ul>
    </div>
  )
}

import { Link } from 'react-router-dom'
import { api } from '../api/client'
import type { Course } from '../api/types'
import { useAsync } from '../api/useAsync'
import { Empty, ErrorNote, Loading } from '../components/ui'

export default function Catalog() {
  const courses = useAsync(() => api<Course[]>('/courses?published=true&limit=100'), [])
  return (
    <div className="page">
      <section className="hero">
        <h1>Aprende a tu ritmo, retoma donde lo dejaste</h1>
        <p>Cursos abiertos con video, lecturas y evaluaciones. Tu avance se guarda solo.</p>
      </section>
      <h2 className="section-title">Cursos disponibles</h2>
      {courses.loading && <Loading />}
      {courses.error && <ErrorNote message={courses.error} onRetry={courses.reload} />}
      {courses.data && courses.data.length === 0 && (
        <Empty title="Todavía no hay cursos publicados">
          <p>Cuando un docente publique el primero, aparecerá aquí.</p>
        </Empty>
      )}
      <ul className="cards">
        {courses.data?.map((c) => (
          <li key={c.id}>
            <Link to={`/cursos/${c.id}`} className="card">
              <h3>{c.title}</h3>
              <p>{c.summary || 'Sin descripción.'}</p>
              <span className="card-cta">Ver curso</span>
            </Link>
          </li>
        ))}
      </ul>
    </div>
  )
}

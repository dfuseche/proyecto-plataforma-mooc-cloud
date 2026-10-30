import { lazy, Suspense, useEffect, useMemo, useRef, useState } from 'react'
import { Link, Navigate, useNavigate, useParams } from 'react-router-dom'
import { api } from '../api/client'
import type { CourseVersion, Enrollment, Resource } from '../api/types'
import { useAsync } from '../api/useAsync'
import Markdown from '../components/Markdown'
import QuizView from '../components/QuizView'
import { ErrorNote, Loading, Progress } from '../components/ui'

// hls.js pesa ~500 kB: solo se descarga al abrir un video o audio.
const MediaPlayer = lazy(() => import('../components/MediaPlayer'))

const TYPE_LABEL: Record<string, string> = {
  text: 'Lectura', video: 'Video', audio: 'Audio', quiz: 'Evaluación', pdf: 'PDF', image: 'Imagen',
  presentation: 'Presentación', download: 'Descarga', link: 'Enlace', iframe: 'Enlace',
}

export default function Learn() {
  const { courseId = '', resourceId } = useParams()
  const navigate = useNavigate()
  const tree = useAsync(() => api<CourseVersion>(`/courses/${courseId}`), [courseId])
  const mine = useAsync(() => api<Enrollment[]>('/learning/enrollments'), [courseId])
  const [progress, setProgress] = useState<number | null>(null)

  const flat = useMemo(() => {
    const out: { resource: Resource; module: string; unit: string }[] = []
    for (const m of tree.data?.modules ?? []) for (const u of m.units ?? []) for (const r of u.resources ?? [])
      if (r.is_visible) out.push({ resource: r, module: m.title, unit: u.title })
    return out
  }, [tree.data])

  const enrollment = mine.data?.find((e) => e.course_id === courseId)
  const pct = progress ?? enrollment?.progress_percentage ?? 0
  const idx = Math.max(0, flat.findIndex((f) => f.resource.id === resourceId))
  const current = flat[idx]

  if ((tree.loading && !tree.data) || (mine.loading && !mine.data)) return <div className="page"><Loading /></div>
  if (tree.error) return <div className="page"><ErrorNote message={tree.error} /></div>
  if (mine.data && !enrollment) return <Navigate to={`/cursos/${courseId}`} replace />
  if (!current) return <div className="page"><p>Este curso todavía no tiene contenido visible.</p></div>
  if (!resourceId) return <Navigate to={`/aprender/${courseId}/${current.resource.id}`} replace />

  const prev = flat[idx - 1]
  const next = flat[idx + 1]
  const go = (r?: { resource: Resource }) => r && navigate(`/aprender/${courseId}/${r.resource.id}`)

  return (
    <div className="learn">
      <aside className="rail" aria-label="Contenido del curso">
        <p className="crumb"><Link to="/mis-cursos">Mis cursos</Link></p>
        <Progress value={pct} label="Avance del curso" />
        <p className="rail-pct">{Math.round(pct)}% completado</p>
        <nav>
          <ol className="route" style={{ ['--progress' as string]: `${pct}%` }}>
            {tree.data?.modules?.map((m) => (
              <li key={m.id} className="route-module">
                <h2>{m.title}</h2>
                {m.units?.map((u) => (
                  <div key={u.id}>
                    <h3>{u.title}</h3>
                    <ul>
                      {u.resources?.filter((r) => r.is_visible).map((r) => (
                        <li key={r.id}>
                          <Link to={`/aprender/${courseId}/${r.id}`} aria-current={r.id === current.resource.id ? 'page' : undefined}>
                            <span>{r.title}</span><small>{TYPE_LABEL[r.type] ?? r.type}</small>
                          </Link>
                        </li>
                      ))}
                    </ul>
                  </div>
                ))}
              </li>
            ))}
          </ol>
        </nav>
      </aside>

      <article className="stage">
        <p className="crumb">{current.module} · {current.unit}</p>
        <h1>{current.resource.title}</h1>
        <ResourceBody key={current.resource.id} courseId={courseId} resource={current.resource} onProgress={(p) => { setProgress(p); mine.reload() }} />
        <div className="pager">
          <button className="btn btn-quiet" disabled={!prev} onClick={() => go(prev)}>Anterior</button>
          <button className="btn" disabled={!next} onClick={() => go(next)}>Siguiente</button>
        </div>
      </article>
    </div>
  )
}

function ResourceBody({ courseId, resource, onProgress }: { courseId: string; resource: Resource; onProgress(pct: number): void }) {
  const lastSent = useRef(0)

  async function beat(position: number, dwell = 15) {
    try {
      const e = await api<Enrollment>('/learning/heartbeat', {
        method: 'POST',
        body: { course_id: courseId, resource_stable_id: resource.stable_id, dwell_time_seconds: dwell, last_position_seconds: position },
      })
      lastSent.current = Date.now()
      onProgress(e.progress_percentage)
    } catch { /* el pulso se reintenta en el siguiente ciclo */ }
  }

  // Recursos sin reproductor cuentan como vistos tras unos segundos de lectura.
  const passive = resource.type !== 'video' && resource.type !== 'audio' && resource.type !== 'quiz'
  useEffect(() => {
    if (!passive) return
    const t = window.setTimeout(() => void beat(0, 12), 12000)
    return () => window.clearTimeout(t)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [resource.id])

  switch (resource.type) {
    case 'video':
    case 'audio':
      return <Suspense fallback={<Loading label="Preparando reproductor…" />}><MediaPlayer resource={resource} onHeartbeat={(p) => void beat(p)} /></Suspense>
    case 'quiz':
      return <QuizView resourceId={resource.id} onFinished={() => void beat(0, 12)} />
    case 'text':
      return <Markdown source={resource.canonical_markdown ?? ''} />
    case 'link':
    case 'iframe':
      return resource.media_url
        ? <p><a className="btn" href={resource.media_url} target="_blank" rel="noreferrer noopener">Abrir enlace</a></p>
        : <p>Este recurso no tiene enlace.</p>
    default:
      return <FileResource resource={resource} />
  }
}

function FileResource({ resource }: { resource: Resource }) {
  const file = useAsync(() => api<{ presigned_url: string }>(`/media/resources/${resource.id}/stream-url`), [resource.id])
  if (file.loading) return <Loading />
  if (file.error || !file.data) return <ErrorNote message={file.error ?? 'No se pudo cargar el archivo.'} onRetry={file.reload} />
  const url = file.data.presigned_url
  return resource.type === 'image'
    ? <img className="media-img" src={url} alt={resource.title} />
    : <p><a className="btn" href={url} target="_blank" rel="noreferrer noopener">Abrir {TYPE_LABEL[resource.type]?.toLowerCase() ?? 'archivo'}</a></p>
}

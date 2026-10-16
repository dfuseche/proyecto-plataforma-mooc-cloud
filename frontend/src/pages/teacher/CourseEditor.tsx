import { useEffect, useState, type FormEvent } from 'react'
import { Link, useParams } from 'react-router-dom'
import { api, ApiError } from '../../api/client'
import type { Course, CourseVersion } from '../../api/types'
import { useAsync } from '../../api/useAsync'
import { ErrorNote, Loading } from '../../components/ui'
import ResourceForm from './ResourceForm'

const TYPE_NAME: Record<string, string> = { text: 'Lectura', video: 'Video', audio: 'Audio', pdf: 'PDF', quiz: 'Evaluación', link: 'Enlace', image: 'Imagen' }
const STATUS: Record<string, string> = { pending: 'Pendiente', processing: 'Procesando', completed: 'Listo', failed: 'Con error' }

export default function CourseEditor() {
  const { courseId = '' } = useParams()
  const course = useAsync(async () => (await api<Course[]>('/courses?limit=200')).find((c) => c.id === courseId) ?? null, [courseId])
  const draft = useAsync(async () => {
    try { return await api<CourseVersion>(`/courses/${courseId}/preview`) } catch (e) {
      if (e instanceof ApiError && e.status === 404) return null
      throw e
    }
  }, [courseId])

  const [moduleTitle, setModuleTitle] = useState('')
  const [unitTitles, setUnitTitles] = useState<Record<string, string>>({})
  const [openUnit, setOpenUnit] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [issues, setIssues] = useState<string[]>([])
  const [published, setPublished] = useState(false)

  const modules = draft.data?.modules ?? []
  const processing = modules.some((m) => m.units?.some((u) => u.resources?.some((r) => r.processing_status === 'processing' && (r.type === 'video' || r.type === 'audio'))))

  // Mientras haya videos transcodificándose, refresca el estado cada 10 s.
  useEffect(() => {
    if (!processing) return
    const id = window.setInterval(draft.reload, 10000)
    return () => window.clearInterval(id)
  }, [processing, draft.reload])

  async function run(fn: () => Promise<unknown>) {
    setBusy(true); setError(null); setIssues([])
    try { await fn(); draft.reload(); course.reload() } catch (e) {
      setError(e instanceof Error ? e.message : 'La operación falló.')
      if (e instanceof ApiError) setIssues(e.details)
    } finally { setBusy(false) }
  }

  const addModule = (e: FormEvent) => {
    e.preventDefault()
    if (!draft.data) return
    void run(async () => {
      await api(`/courses/versions/${draft.data!.id}/modules`, { method: 'POST', body: { title: moduleTitle.trim(), description: '', position: modules.length + 1 } })
      setModuleTitle('')
    })
  }
  const addUnit = (moduleId: string, count: number) => {
    const t = (unitTitles[moduleId] ?? '').trim()
    if (!t) return
    void run(async () => {
      await api(`/courses/modules/${moduleId}/units`, { method: 'POST', body: { title: t, position: count + 1 } })
      setUnitTitles({ ...unitTitles, [moduleId]: '' })
    })
  }
  const publish = () => void run(async () => {
    await api(`/courses/versions/${draft.data!.id}/publish`, { method: 'POST' })
    setPublished(true)
  })
  const createDraft = () => void run(() => api(`/courses/${courseId}/create-draft`, { method: 'POST' }))

  if (course.loading || draft.loading) return <div className="page"><Loading /></div>
  if (draft.error) return <div className="page"><ErrorNote message={draft.error} onRetry={draft.reload} /></div>

  const isPublished = !!course.data?.current_published_version_id

  return (
    <div className="page">
      <p className="crumb"><Link to="/docente">Docencia</Link></p>
      <h1>{course.data?.title ?? 'Curso'}</h1>
      {published && <div className="note note-ok" role="status"><p>Curso publicado. Ya aparece en el catálogo.</p></div>}

      {!draft.data ? (
        <div className="note">
          <p>{isPublished ? 'Este curso está publicado y su contenido no se puede editar. Crea un borrador para preparar una actualización.' : 'Este curso no tiene un borrador editable.'}</p>
          {isPublished && <button className="btn" onClick={createDraft} disabled={busy}>Crear borrador de actualización</button>}
        </div>
      ) : (
        <>
          <div className="actions">
            <button className="btn" onClick={publish} disabled={busy || modules.length === 0}>Publicar versión</button>
            <button className="btn btn-quiet" onClick={draft.reload}>Actualizar estado</button>
            {processing && <span className="muted">Hay recursos procesándose…</span>}
          </div>
          {error && <ErrorNote message={error} />}
          {issues.length > 0 && <ul className="issues">{issues.map((i) => <li key={i}>{i}</li>)}</ul>}

          <h2 className="section-title">Estructura</h2>
          {modules.length === 0 && <p>Empieza añadiendo un módulo.</p>}
          {modules.map((m) => (
            <section key={m.id} className="module">
              <h3>{m.title}</h3>
              {m.units?.map((u) => (
                <div key={u.id} className="unit">
                  <h4>{u.title}</h4>
                  <ul className="rows">
                    {u.resources?.map((r) => (
                      <li key={r.id}><span>{r.title}</span><span className="tag">{TYPE_NAME[r.type] ?? r.type}{(r.type === 'video' || r.type === 'audio') ? ` · ${STATUS[r.processing_status] ?? r.processing_status}` : ''}</span></li>
                    ))}
                  </ul>
                  {openUnit === u.id ? (
                    <ResourceForm unitId={u.id} position={(u.resources?.length ?? 0) + 1} onDone={() => { setOpenUnit(null); draft.reload() }} />
                  ) : (
                    <button className="btn btn-quiet" onClick={() => setOpenUnit(u.id)}>Añadir recurso</button>
                  )}
                </div>
              ))}
              <div className="inline-add">
                <label htmlFor={`u-${m.id}`} className="sr-only">Título de la nueva unidad</label>
                <input id={`u-${m.id}`} placeholder="Título de la nueva unidad" value={unitTitles[m.id] ?? ''} onChange={(e) => setUnitTitles({ ...unitTitles, [m.id]: e.target.value })} />
                <button className="btn btn-quiet" onClick={() => addUnit(m.id, m.units?.length ?? 0)} disabled={busy}>Añadir unidad</button>
              </div>
            </section>
          ))}
          <form onSubmit={addModule} className="inline-add module-add">
            <label htmlFor="m-title" className="sr-only">Título del nuevo módulo</label>
            <input id="m-title" placeholder="Título del nuevo módulo" value={moduleTitle} onChange={(e) => setModuleTitle(e.target.value)} required />
            <button className="btn" disabled={busy}>Añadir módulo</button>
          </form>
        </>
      )}
    </div>
  )
}

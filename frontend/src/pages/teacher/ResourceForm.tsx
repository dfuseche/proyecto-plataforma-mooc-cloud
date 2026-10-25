import { useState, type FormEvent } from 'react'
import { api } from '../../api/client'
import type { Resource, ResourceType } from '../../api/types'
import { ErrorNote } from '../../components/ui'
import QuizBuilder from './QuizBuilder'

const TYPES: { value: ResourceType; label: string }[] = [
  { value: 'text', label: 'Lectura' },
  { value: 'video', label: 'Video' },
  { value: 'audio', label: 'Audio' },
  { value: 'pdf', label: 'PDF' },
  { value: 'quiz', label: 'Evaluación' },
  { value: 'link', label: 'Enlace externo' },
]
const CONTENT_TYPE: Partial<Record<ResourceType, string>> = { video: 'video/mp4', audio: 'audio/mpeg', pdf: 'application/pdf' }
const ACCEPT: Partial<Record<ResourceType, string>> = { video: 'video/*', audio: 'audio/*', pdf: 'application/pdf' }

// PUT con progreso a la URL prefirmada (la API no ve los bytes del archivo).
function putFile(url: string, file: File, contentType: string, onProgress: (pct: number) => void): Promise<void> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest()
    xhr.open('PUT', url)
    xhr.setRequestHeader('Content-Type', contentType)
    xhr.upload.onprogress = (e) => { if (e.lengthComputable) onProgress(Math.round((e.loaded / e.total) * 100)) }
    xhr.onload = () => (xhr.status >= 200 && xhr.status < 300 ? resolve() : reject(new Error(`El almacenamiento rechazó la subida (código ${xhr.status}).`)))
    xhr.onerror = () => reject(new Error('Falló la subida del archivo. Revisa tu conexión.'))
    xhr.send(file)
  })
}

export default function ResourceForm({ unitId, position, onDone }: { unitId: string; position: number; onDone(): void }) {
  const [title, setTitle] = useState('')
  const [type, setType] = useState<ResourceType>('text')
  const [markdown, setMarkdown] = useState('')
  const [url, setUrl] = useState('')
  const [file, setFile] = useState<File | null>(null)
  const [mandatory, setMandatory] = useState(true)
  const [pct, setPct] = useState<number | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [quizFor, setQuizFor] = useState<string | null>(null)

  const needsFile = type === 'video' || type === 'audio' || type === 'pdf'

  async function submit(e: FormEvent) {
    e.preventDefault()
    if (needsFile && !file) { setError('Elige el archivo que quieres subir.'); return }
    setBusy(true); setError(null); setPct(null)
    try {
      const res = await api<Resource>(`/courses/units/${unitId}/resources`, {
        method: 'POST',
        body: {
          title: title.trim(), type, position,
          canonical_markdown: type === 'text' ? markdown : undefined,
          media_url: type === 'link' ? url.trim() : undefined,
          is_visible: true, is_mandatory: mandatory,
        },
      })
      if (needsFile && file && res.presigned_upload_url) {
        setPct(0)
        await putFile(res.presigned_upload_url, file, CONTENT_TYPE[type] ?? 'application/octet-stream', setPct)
        await api(`/media/resources/${res.id}/complete-upload`, { method: 'POST', body: { object_key: res.object_key } })
      }
      if (type === 'quiz') { setQuizFor(res.id); return }
      onDone()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'No se pudo crear el recurso.')
    } finally { setBusy(false) }
  }

  if (quizFor) return <QuizBuilder resourceId={quizFor} onSaved={onDone} />

  return (
    <form onSubmit={submit} className="form inline-form">
      <div className="two">
        <div className="field"><label htmlFor="r-title">Título del recurso</label><input id="r-title" value={title} onChange={(e) => setTitle(e.target.value)} required /></div>
        <div className="field">
          <label htmlFor="r-type">Tipo</label>
          <select id="r-type" value={type} onChange={(e) => { setType(e.target.value as ResourceType); setFile(null) }}>
            {TYPES.map((t) => <option key={t.value} value={t.value}>{t.label}</option>)}
          </select>
        </div>
      </div>
      {type === 'text' && (
        <div className="field">
          <label htmlFor="r-md">Contenido</label>
          <textarea id="r-md" rows={6} value={markdown} onChange={(e) => setMarkdown(e.target.value)} required />
          <p className="hint">Admite títulos con #, listas con -, **negrita**, *cursiva* y enlaces [texto](https://…).</p>
        </div>
      )}
      {type === 'link' && (
        <div className="field"><label htmlFor="r-url">Dirección del enlace</label><input id="r-url" type="url" value={url} onChange={(e) => setUrl(e.target.value)} placeholder="https://" required /></div>
      )}
      {needsFile && (
        <div className="field">
          <label htmlFor="r-file">Archivo</label>
          <input id="r-file" type="file" accept={ACCEPT[type]} onChange={(e) => setFile(e.target.files?.[0] ?? null)} />
          {type !== 'pdf' && <p className="hint">Se transcodifica a varias calidades; verás el estado “Procesando” hasta que termine.</p>}
        </div>
      )}
      <label className="check"><input type="checkbox" checked={mandatory} onChange={(e) => setMandatory(e.target.checked)} /> Obligatorio para completar el curso</label>
      {pct !== null && <div className="progress" role="progressbar" aria-valuenow={pct} aria-valuemin={0} aria-valuemax={100} aria-label="Subida del archivo"><span style={{ width: `${pct}%` }} /></div>}
      {error && <ErrorNote message={error} />}
      <button type="submit" className="btn" disabled={busy}>{busy ? (pct !== null ? `Subiendo… ${pct}%` : 'Guardando…') : type === 'quiz' ? 'Crear y definir preguntas' : 'Añadir recurso'}</button>
    </form>
  )
}

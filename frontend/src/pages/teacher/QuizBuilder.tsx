import { useState } from 'react'
import { api } from '../../api/client'
import { ErrorNote } from '../../components/ui'

interface Q { text: string; options: string[]; correct: number }
const blank = (): Q => ({ text: '', options: ['', ''], correct: 0 })

export default function QuizBuilder({ resourceId, onSaved }: { resourceId: string; onSaved(): void }) {
  const [qs, setQs] = useState<Q[]>([blank()])
  const [attempts, setAttempts] = useState('3')
  const [passing, setPassing] = useState('70')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const update = (i: number, patch: Partial<Q>) => setQs(qs.map((q, j) => (j === i ? { ...q, ...patch } : q)))

  async function save() {
    setBusy(true); setError(null)
    try {
      await api(`/courses/resources/${resourceId}/quiz`, {
        method: 'POST',
        body: {
          max_attempts: Number(attempts),
          passing_score: Number(passing),
          questions: qs.map((q, i) => ({
            question_text: q.text.trim(),
            position: i + 1,
            points: 100 / qs.length,
            options: q.options.map((o, j) => ({ option_text: o.trim(), is_correct: j === q.correct, position: j + 1 })),
          })),
        },
      })
      onSaved()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'No se pudo guardar la evaluación.')
    } finally { setBusy(false) }
  }

  const valid = qs.every((q) => q.text.trim() && q.options.every((o) => o.trim()))

  return (
    <div className="builder">
      <h4>Preguntas de la evaluación</h4>
      {qs.map((q, i) => (
        <fieldset key={i}>
          <legend>Pregunta {i + 1}</legend>
          <div className="field">
            <label htmlFor={`q-${i}`}>Enunciado</label>
            <input id={`q-${i}`} value={q.text} onChange={(e) => update(i, { text: e.target.value })} />
          </div>
          {q.options.map((o, j) => (
            <div className="opt-row" key={j}>
              <input type="radio" name={`c-${i}`} checked={q.correct === j} onChange={() => update(i, { correct: j })} aria-label={`Marcar opción ${j + 1} como correcta`} />
              <input value={o} onChange={(e) => update(i, { options: q.options.map((x, k) => (k === j ? e.target.value : x)) })} aria-label={`Opción ${j + 1} de la pregunta ${i + 1}`} placeholder={`Opción ${j + 1}`} />
            </div>
          ))}
          <div className="row-actions">
            {q.options.length < 5 && <button type="button" className="btn btn-quiet" onClick={() => update(i, { options: [...q.options, ''] })}>Añadir opción</button>}
            {qs.length > 1 && <button type="button" className="btn btn-quiet" onClick={() => setQs(qs.filter((_, k) => k !== i))}>Quitar pregunta</button>}
          </div>
        </fieldset>
      ))}
      <button type="button" className="btn btn-quiet" onClick={() => setQs([...qs, blank()])}>Añadir pregunta</button>
      <div className="two">
        <div className="field"><label htmlFor="qa">Intentos permitidos</label><input id="qa" type="number" min={1} value={attempts} onChange={(e) => setAttempts(e.target.value)} /></div>
        <div className="field"><label htmlFor="qp">Puntaje para aprobar</label><input id="qp" type="number" min={0} max={100} value={passing} onChange={(e) => setPassing(e.target.value)} /></div>
      </div>
      {error && <ErrorNote message={error} />}
      <button type="button" className="btn" disabled={!valid || busy} onClick={save}>{busy ? 'Guardando…' : 'Guardar evaluación'}</button>
    </div>
  )
}

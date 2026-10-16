import { useEffect, useState } from 'react'
import { api, ApiError } from '../api/client'
import type { QuizAttempt, QuizSnapshot } from '../api/types'
import { useAsync } from '../api/useAsync'
import { ErrorNote, Loading } from './ui'

export default function QuizView({ resourceId, onFinished }: { resourceId: string; onFinished(): void }) {
  const quiz = useAsync(() => api<QuizSnapshot>(`/learning/resources/${resourceId}/quiz`), [resourceId])
  const [attempt, setAttempt] = useState<QuizAttempt | null>(null)
  const [answers, setAnswers] = useState<Record<string, string>>({})
  const [result, setResult] = useState<QuizAttempt | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => { setAttempt(null); setResult(null); setAnswers({}); setError(null) }, [resourceId])

  if (quiz.loading) return <Loading label="Cargando evaluación…" />
  if (quiz.error || !quiz.data) return <ErrorNote message={quiz.error ?? 'No hay evaluación disponible.'} onRetry={quiz.reload} />
  const q = quiz.data

  async function start() {
    setBusy(true); setError(null)
    try {
      setAttempt(await api<QuizAttempt>(`/learning/quizzes/${q.id}/start-attempt`, { method: 'POST', body: {} }))
    } catch (e) {
      setError(e instanceof ApiError && e.status === 409
        ? `Ya usaste los ${q.max_attempts} intentos permitidos.`
        : e instanceof Error ? e.message : 'No se pudo iniciar el intento.')
    } finally { setBusy(false) }
  }

  async function choose(questionId: string, optionId: string) {
    const next = { ...answers, [questionId]: optionId }
    setAnswers(next)
    if (attempt) api(`/learning/attempts/${attempt.id}`, { method: 'PATCH', body: { answers: next } }).catch(() => undefined)
  }

  async function submit() {
    if (!attempt) return
    setBusy(true); setError(null)
    try {
      const res = await api<QuizAttempt>(`/learning/attempts/${attempt.id}/submit`, { method: 'POST', body: { answers } })
      setResult(res)
      onFinished()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'No se pudo enviar el intento.')
    } finally { setBusy(false) }
  }

  if (result) {
    const score = result.score ?? 0
    const passed = score >= q.passing_score
    return (
      <div className={`note ${passed ? 'note-ok' : ''}`} role="status">
        <h3>{passed ? 'Aprobaste la evaluación' : 'Aún no alcanzas el puntaje'}</h3>
        <p>Tu puntaje: <strong>{Math.round(score)}</strong> de 100. Se necesitan {q.passing_score}.</p>
        {!passed && <button className="btn" onClick={() => { setAttempt(null); setResult(null); setAnswers({}) }}>Intentar de nuevo</button>}
      </div>
    )
  }

  if (!attempt) {
    return (
      <div className="note">
        <h3>Evaluación</h3>
        <p>{q.questions.length} preguntas. Necesitas {q.passing_score} puntos para aprobar y tienes hasta {q.max_attempts} intentos.</p>
        {error && <p className="field-error" role="alert">{error}</p>}
        <button className="btn" onClick={start} disabled={busy}>Empezar intento</button>
      </div>
    )
  }

  const complete = q.questions.every((x) => answers[x.id])
  return (
    <form className="quiz" onSubmit={(e) => { e.preventDefault(); void submit() }}>
      {q.questions.map((question, i) => (
        <fieldset key={question.id}>
          <legend>{i + 1}. {question.question_text}</legend>
          {question.options.map((o) => (
            <label key={o.id} className="choice">
              <input type="radio" name={question.id} checked={answers[question.id] === o.id} onChange={() => void choose(question.id, o.id)} />
              <span>{o.option_text}</span>
            </label>
          ))}
        </fieldset>
      ))}
      {error && <p className="field-error" role="alert">{error}</p>}
      <button className="btn" type="submit" disabled={!complete || busy}>Enviar respuestas</button>
      {!complete && <p className="hint">Responde todas las preguntas para enviar.</p>}
    </form>
  )
}

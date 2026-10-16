import type { ReactNode } from 'react'

export function Loading({ label = 'Cargando…' }: { label?: string }) {
  return <p className="state" role="status" aria-live="polite">{label}</p>
}

export function ErrorNote({ message, onRetry }: { message: string; onRetry?: () => void }) {
  return (
    <div className="note note-error" role="alert">
      <p>{message}</p>
      {onRetry && <button className="btn btn-quiet" onClick={onRetry}>Reintentar</button>}
    </div>
  )
}

export function Empty({ title, children }: { title: string; children?: ReactNode }) {
  return (
    <div className="empty">
      <h3>{title}</h3>
      {children}
    </div>
  )
}

export function Field(props: {
  label: string; name: string; type?: string; value: string; onChange(v: string): void
  autoComplete?: string; hint?: string; required?: boolean; minLength?: number
}) {
  const id = `f-${props.name}`
  return (
    <div className="field">
      <label htmlFor={id}>{props.label}</label>
      <input
        id={id} name={props.name} type={props.type ?? 'text'} value={props.value}
        onChange={(e) => props.onChange(e.target.value)} autoComplete={props.autoComplete}
        required={props.required ?? true} minLength={props.minLength}
        aria-describedby={props.hint ? `${id}-hint` : undefined}
      />
      {props.hint && <p id={`${id}-hint`} className="hint">{props.hint}</p>}
    </div>
  )
}

/** Barra de avance accesible. */
export function Progress({ value, label }: { value: number; label: string }) {
  const v = Math.max(0, Math.min(100, Math.round(value)))
  return (
    <div className="progress" role="progressbar" aria-valuemin={0} aria-valuemax={100} aria-valuenow={v} aria-label={label}>
      <span style={{ width: `${v}%` }} />
    </div>
  )
}

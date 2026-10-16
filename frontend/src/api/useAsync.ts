import { useCallback, useEffect, useState } from 'react'

export interface AsyncState<T> { data: T | null; error: string | null; loading: boolean; reload(): void }

// Carga un recurso al montar (o cuando cambian las dependencias) y permite recargarlo.
export function useAsync<T>(fn: () => Promise<T>, deps: unknown[]): AsyncState<T> {
  const [data, setData] = useState<T | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const [tick, setTick] = useState(0)

  useEffect(() => {
    let alive = true
    setLoading(true)
    setError(null)
    fn()
      .then((d) => { if (alive) setData(d) })
      .catch((e: unknown) => { if (alive) setError(e instanceof Error ? e.message : 'Error inesperado') })
      .finally(() => { if (alive) setLoading(false) })
    return () => { alive = false }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [...deps, tick])

  const reload = useCallback(() => setTick((t) => t + 1), [])
  return { data, error, loading, reload }
}

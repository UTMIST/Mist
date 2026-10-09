import { useCallback, useEffect, useState } from 'react'

export function errorMessage(error: unknown) {
  return error instanceof Error ? error.message : String(error)
}

// One in-flight request per consumer, cancellation on unmount, and automatic
// recovery. Refresh aborts the old request so late responses cannot win.
export function usePolling<T>(
  load: (signal: AbortSignal) => Promise<T>,
  intervalMs = 5000,
) {
  const [data, setData] = useState<T | null>(null)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(true)
  const [revision, setRevision] = useState(0)
  const refresh = useCallback(() => setRevision((value) => value + 1), [])
  useEffect(() => {
    const controller = new AbortController()
    let running = false
    async function poll() {
      if (running) return
      running = true
      try {
        const response = await load(controller.signal)
        if (!controller.signal.aborted) {
          setData(response)
          setError('')
          setLoading(false)
        }
      } catch (err) {
        if (!controller.signal.aborted) {
          setError(errorMessage(err))
          setLoading(false)
        }
      } finally {
        running = false
      }
    }
    void poll()
    const interval = setInterval(() => {
      void poll()
    }, intervalMs)
    return () => {
      controller.abort()
      clearInterval(interval)
    }
  }, [load, intervalMs, revision])
  return { data, error, loading, refresh }
}

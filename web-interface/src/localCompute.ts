import { useCallback, useEffect, useState } from 'react'

export type Hardware = {
  available: boolean
  name?: string
  memory_total_mib?: number
  memory_used_mib?: number
  utilization_percent?: number
  driver_version?: string
  error?: string
}

export type ComputeJob = {
  id: string
  created: string
  job_state: string
  payload: { matrix_size?: number }
  error?: string
  logs?: string
  result?: {
    gpu: string
    gpu_ms: number
    cpu_ms: number
    speedup: number
    peak_vram_mib: number
    gpu_iterations: number
    cpu_iterations: number
    cpu_threads: number
  }
}

export async function api<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch('/api' + path, {
    ...init,
    signal: AbortSignal.timeout(25000),
  })
  if (!response.ok)
    throw new Error(
      (await response.text()).trim() ||
        'Request failed (' + response.status + ')',
    )
  if (!response.headers.get('content-type')?.includes('application/json'))
    throw new Error(
      'The compute API is not connected. Start the local MIST runner.',
    )
  return response.json()
}

export function useHardware() {
  const [hardware, setHardware] = useState<Hardware | null>(null)
  const [error, setError] = useState('')
  useEffect(() => {
    let stopped = false
    let timer: ReturnType<typeof setTimeout>
    async function poll() {
      try {
        const result = await api<Hardware>('/hardware')
        if (!stopped) {
          setHardware(result)
          setError('')
        }
      } catch (err) {
        if (!stopped)
          setError(
            err instanceof Error ? err.message : 'Cannot reach local worker',
          )
      }
      if (!stopped) timer = setTimeout(poll, 15000)
    }
    void poll()
    return () => {
      stopped = true
      clearTimeout(timer)
    }
  }, [])
  return { hardware, error }
}

export function useJobs() {
  const [jobs, setJobs] = useState<ComputeJob[]>([])
  const [error, setError] = useState('')
  const refresh = useCallback(async () => {
    try {
      const result = await api<{ jobs: ComputeJob[] }>('/jobs')
      setJobs(result.jobs)
      setError('')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Cannot read job history')
    }
  }, [])
  useEffect(() => {
    void refresh()
    const timer = setInterval(refresh, 1500)
    return () => clearInterval(timer)
  }, [refresh])
  return { jobs, error, refresh }
}

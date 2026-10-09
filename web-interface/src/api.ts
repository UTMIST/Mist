export type Compute = 'cpu' | 'nvidia' | 'tenstorrent'

export type Job = {
  id: string
  name: string
  created: string
  job_state: 'Scheduled' | 'InProgress' | 'Success' | 'Failure' | 'Cancelled'
  accelerator: Compute
  device_count: number
  image: string
  cpu: string
  memory: string
  node?: string
  exit_code?: number
  message?: string
  checkpoint_directory: string
}

export type Submission = {
  name?: string
  type: 'command' | 'training-smoke'
  accelerator: Compute
  device_count?: number
  script?: string
  script_name?: string
  image?: string
  cpu?: string
  memory?: string
  timeout_seconds?: number
}

const base = (import.meta.env.VITE_API_URL ?? '/api').replace(/\/$/, '')

async function request<T>(path: string, options?: RequestInit): Promise<T> {
  const response = await fetch(`${base}${path}`, {
    ...options,
    headers: { 'Content-Type': 'application/json', ...options?.headers },
  })
  const body = await response.json()
  if (!response.ok) throw new Error(body.error ?? `Request failed (${response.status})`)
  return body as T
}

export const jobsAPI = {
  list: () => request<{ jobs: Job[]; count: number }>('/jobs'),
  submit: (submission: Submission) => request<{ job_id: string; job: Job }>('/jobs', {
    method: 'POST', body: JSON.stringify(submission),
  }),
  cancel: (id: string) => request<Job>(`/jobs/${encodeURIComponent(id)}/cancel`, { method: 'POST' }),
  logs: (id: string) => request<{ logs: string }>(`/jobs/${encodeURIComponent(id)}/logs`),
}

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
  output_directory?: string
  working_directory?: string
  timeout_seconds?: number
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
  command?: string[]
  args?: string[]
  env?: Record<string, string>
  working_directory?: string
}

export type MachineHardware = {
  name: string
  address: string
  ready: boolean
  schedulable: boolean
  cpu_allocatable: string
  memory_allocatable: string
}

export type AcceleratorPool = {
  accelerator: Exclude<Compute, 'cpu'>
  node: string
  unit: string
  total: number
  allocated: number
  available: number | null
  ready: boolean
  chips_per_device?: number
  message?: string
}

export type HardwareSnapshot = {
  observed_at: string
  machines: MachineHardware[]
  pools: AcceleratorPool[]
}

export type ImageCatalog = {
  images: { reference: string; accelerators: Compute[] }[]
  profiles: {
    accelerator: Compute
    default_image: string
    device_unit: string
    max_devices: number
    note?: string
  }[]
}

const base = (import.meta.env.VITE_API_URL ?? '/api').replace(/\/$/, '')

async function request<T>(path: string, options?: RequestInit): Promise<T> {
  const response = await fetch(`${base}${path}`, {
    ...options,
    headers: { 'Content-Type': 'application/json', ...options?.headers },
  })
  let body
  try {
    body = await response.json()
  } catch {
    throw new Error(
      `Mist API returned HTTP ${response.status}. Check the API connection.`,
    )
  }
  if (!response.ok)
    throw new Error(body.error ?? `Request failed (${response.status})`)
  return body as T
}

export const jobsAPI = {
  list: (signal?: AbortSignal) =>
    request<{ jobs: Job[]; count: number }>('/jobs', { signal }),
  hardware: (signal?: AbortSignal) =>
    request<HardwareSnapshot>('/hardware', { signal }),
  images: (signal?: AbortSignal) =>
    request<ImageCatalog>('/images', { signal }),
  submit: (submission: Submission) =>
    request<{ job_id: string; job: Job }>('/jobs', {
      method: 'POST',
      body: JSON.stringify(submission),
    }),
  cancel: (id: string) =>
    request<Job>(`/jobs/${encodeURIComponent(id)}/cancel`, { method: 'POST' }),
  logs: (id: string, signal?: AbortSignal) =>
    request<{ logs: string }>(`/jobs/${encodeURIComponent(id)}/logs`, {
      signal,
    }),
}

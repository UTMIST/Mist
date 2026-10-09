import type { Member } from '#/auth.tsx'

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
  team_id?: string
  creator_id?: string
  creator_name?: string
  storage_scope?: string
  can_cancel?: boolean
}

export type Submission = {
  name?: string
  dataset_id?: string
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
  storage_scope?: string
  tt_runtime?: 'host' | 'container'
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
  self_service?: boolean
  teams_enabled?: boolean
  registries?: string[]
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
let activeTeam = ''
export function setActiveTeam(id: string) {
  activeTeam = id
}
function teamQuery(): Record<string, string> {
  return activeTeam ? { team_id: activeTeam } : {}
}

async function request<T>(path: string, options?: RequestInit): Promise<T> {
  const requestTeam = activeTeam
  const response = await fetch(`${base}${path}`, {
    ...options,
    headers: {
      'Content-Type': 'application/json',
      ...(requestTeam ? { 'X-Mist-Team': requestTeam } : {}),
      ...options?.headers,
    },
  })
  if (response.status === 401 && path !== '/session')
    window.dispatchEvent(new Event('mist:sign-in-required'))
  if (
    response.status === 403 &&
    requestTeam &&
    (!options?.method || options.method === 'GET') &&
    !path.startsWith('/teams')
  )
    window.dispatchEvent(
      new CustomEvent('mist:workspace-access-revoked', { detail: requestTeam }),
    )
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
  session: (signal?: AbortSignal) =>
    request<{ enabled: boolean; user: Member | null }>('/session', { signal }),
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

export type Dataset = {
  id: string
  name: string
  filename: string
  size: number
  files: number
  sha256: string
  created: string
  state: string
  team_id?: string
  team_name?: string
  scope?: string
  member_name?: string
  shared?: boolean
}
export type ResultFile = { path: string; size: number; modified: string }
export const storageAPI = {
  list: (signal?: AbortSignal) =>
    request<{ datasets: Dataset[]; upload_limit_bytes: number }>('/datasets', {
      signal,
    }),
  info: (signal?: AbortSignal) =>
    request<{
      enabled: boolean
      capacity_bytes: number
      available_bytes: number
      upload_limit_bytes: number
    }>('/storage', { signal }),
  remove: (id: string) =>
    request(`/datasets/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  files: (id: string, signal?: AbortSignal) =>
    request<{ files: ResultFile[] }>(`/jobs/${encodeURIComponent(id)}/files`, {
      signal,
    }),
  downloadURL: (id: string, path: string) =>
    `${base}/jobs/${encodeURIComponent(id)}/files/download?${new URLSearchParams({ path, ...teamQuery() })}`,
  datasetFiles: (id: string, signal?: AbortSignal) =>
    request<{ files: ResultFile[] }>(
      `/datasets/${encodeURIComponent(id)}/files`,
      { signal },
    ),
  datasetDownloadURL: (id: string, path: string) =>
    `${base}/datasets/${encodeURIComponent(id)}/files/download?${new URLSearchParams({ path, ...teamQuery() })}`,
  folders: (signal?: AbortSignal) =>
    request<{ folders: StorageFolder[] }>('/storage/folders', { signal }),
  folderFiles: (folder: StorageFolder, signal?: AbortSignal) =>
    request<{ files: ResultFile[] }>(
      `/storage/files?${new URLSearchParams({ source_team: folder.team_id, scope: folder.scope })}`,
      { signal },
    ),
  folderDownloadURL: (folder: StorageFolder, path: string) =>
    `${base}/storage/files?${new URLSearchParams({ source_team: folder.team_id, scope: folder.scope, path, download: 'true', ...teamQuery() })}`,
  upload: (
    file: File,
    name: string,
    zip: boolean,
    progress: (value: number) => void,
    scope?: string,
    signal?: AbortSignal,
  ) =>
    new Promise<Dataset>((resolve, reject) => {
      const xhr = new XMLHttpRequest()
      const abort = () => xhr.abort()
      if (signal?.aborted) {
        reject(new Error('Upload cancelled'))
        return
      }
      signal?.addEventListener('abort', abort, { once: true })
      xhr.onloadend = () => signal?.removeEventListener('abort', abort)
      xhr.onabort = () => reject(new Error('Upload cancelled'))
      xhr.open(
        'POST',
        `${base}/datasets?${new URLSearchParams({ filename: file.name, name, ...(scope ? { scope } : {}), ...(zip ? { format: 'zip' } : {}) })}`,
      )
      xhr.timeout = 4 * 60 * 60 * 1000
      if (activeTeam) xhr.setRequestHeader('X-Mist-Team', activeTeam)
      xhr.upload.onprogress = (e) => {
        if (e.lengthComputable) progress(Math.round((100 * e.loaded) / e.total))
      }
      xhr.onerror = () => reject(new Error('Upload connection failed'))
      xhr.ontimeout = () => reject(new Error('Upload timed out'))
      xhr.onload = () => {
        if (xhr.status === 401)
          window.dispatchEvent(new Event('mist:sign-in-required'))
        try {
          const body = JSON.parse(xhr.responseText)
          if (xhr.status >= 200 && xhr.status < 300) resolve(body)
          else reject(new Error(body.error ?? `Upload failed (${xhr.status})`))
        } catch {
          reject(new Error('Invalid upload response'))
        }
      }
      xhr.send(file)
    }),
}

export type TeamPolicy = {
  cpu: string
  memory: string
  nvidia: number
  tenstorrent: number
  concurrent: number
  queued: number
  storage_gib: number
  runtime_seconds: number
  registries: string[]
}
export type TeamMember = {
  id: string
  name: string
  email: string
  common_writer: boolean
}
export type StorageGrant = { id: string; target_team: string; scope: string }
export type Team = {
  id: string
  name: string
  disabled: boolean
  policy: TeamPolicy
  members: TeamMember[]
  grants: StorageGrant[]
}
export type StorageFolder = {
  team_id: string
  team_name: string
  scope: string
  name: string
  shared: boolean
  writable: boolean
}
export const teamsAPI = {
  list: (signal?: AbortSignal) =>
    request<{ teams: Team[]; storage_budget_gib: number }>('/teams', {
      signal,
    }),
  create: (name: string, storage_gib = 10) =>
    request<Team>('/teams', {
      method: 'POST',
      body: JSON.stringify({ name, storage_gib }),
    }),
  update: (
    id: string,
    change: { name?: string; disabled?: boolean; policy?: TeamPolicy },
  ) =>
    request<Team>(`/teams/${id}`, {
      method: 'PATCH',
      body: JSON.stringify(change),
    }),
  enroll: (id: string, user_id: string, common_writer: boolean) =>
    request<Team>(`/teams/${id}/members`, {
      method: 'POST',
      body: JSON.stringify({ user_id, common_writer }),
    }),
  removeMember: (id: string, user: string) =>
    request<Team>(`/teams/${id}/members/${encodeURIComponent(user)}`, {
      method: 'DELETE',
    }),
  grant: (id: string, target_team: string, scope: string) =>
    request<Team>(`/teams/${id}/grants`, {
      method: 'POST',
      body: JSON.stringify({ target_team, scope }),
    }),
  revoke: (id: string, grant: string) =>
    request<Team>(`/teams/${id}/grants/${grant}`, { method: 'DELETE' }),
}

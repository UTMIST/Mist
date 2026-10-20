// @vitest-environment jsdom
import { afterEach, beforeEach, expect, test, vi } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { DashboardPage } from './DashboardPage'
import { jobsAPI } from '../api'
import type { Job } from '../api'

vi.mock('@tanstack/react-router', async () => {
  const actual = await vi.importActual<any>('@tanstack/react-router')
  return {
    ...actual,
    Link: ({ children, to, className }: any) => (
      <a href={typeof to === 'string' ? to : '/'} className={className}>
        {children}
      </a>
    ),
  }
})

vi.mock('../api', () => ({
  jobsAPI: {
    listCluster: vi.fn(),
    hardware: vi.fn(),
    logs: vi.fn(),
  },
}))

vi.mock('../auth.tsx', () => ({
  useAccount: () => ({
    user: { id: 'alice', name: 'Alice', email: 'alice@mist.local', role: 'user' },
    isAdmin: false,
    enabled: true,
  }),
  isUserAdmin: (u: { role?: string } | null) => Boolean(u?.role?.includes('admin')),
}))

vi.mock('../teams.tsx', () => ({
  useWorkspace: () => ({
    workspace: { id: 'team-nlp', name: 'UTMIST NLP', type: 'team' },
    workspaces: [],
  }),
  useTeam: () => ({
    workspace: { id: 'team-nlp', name: 'UTMIST NLP', type: 'team' },
    workspaces: [],
  }),
}))

let jobs: Job[]
beforeEach(() => {
  vi.resetAllMocks()
  HTMLDialogElement.prototype.showModal = function () {
    this.setAttribute('open', '')
  }
  HTMLDialogElement.prototype.close = function () {
    this.removeAttribute('open')
  }

  jobs = [
    {
      id: 'job-bob',
      name: 'Bobs training',
      created: '2026-10-20T10:00:00Z',
      job_state: 'InProgress',
      accelerator: 'nvidia',
      device_count: 1,
      cpu: '4',
      memory: '8Gi',
      image: 'mist/nvidia',
      checkpoint_directory: '/outputs',
      creator_id: 'bob',
      creator_name: 'Bob',
      node: 'node-gpu-1',
    },
    {
      id: 'job-alice',
      name: 'Alices fine-tuning',
      created: '2026-10-20T10:30:00Z',
      job_state: 'Scheduled',
      accelerator: 'tenstorrent',
      device_count: 2,
      cpu: '8',
      memory: '16Gi',
      image: 'mist/tt',
      checkpoint_directory: '/outputs',
      creator_id: 'alice',
      creator_name: 'Alice',
      message: 'Waiting in cluster fair queue',
    },
  ]

  vi.mocked(jobsAPI.listCluster).mockResolvedValue({
    jobs,
    count: jobs.length,
  })

  vi.mocked(jobsAPI.hardware).mockResolvedValue({
    observed_at: '2026-10-20T12:00:00Z',
    machines: [
      {
        name: 'node-gpu-1',
        address: '10.0.0.1',
        ready: true,
        schedulable: true,
        cpu_allocatable: '16',
        memory_allocatable: '64Gi',
      },
      {
        name: 'node-tt-1',
        address: '10.0.0.2',
        ready: true,
        schedulable: true,
        cpu_allocatable: '32',
        memory_allocatable: '128Gi',
      },
    ],
    pools: [
      {
        accelerator: 'nvidia',
        node: 'node-gpu-1',
        unit: 'GPU',
        total: 2,
        allocated: 1,
        available: 1,
        ready: true,
      },
      {
        accelerator: 'tenstorrent',
        node: 'node-tt-1',
        unit: 'board',
        total: 4,
        allocated: 2,
        available: 2,
        ready: true,
      },
    ],
  })

  vi.mocked(jobsAPI.logs).mockResolvedValue({
    logs: 'Epoch 1/10 complete. Loss: 0.123',
  })
})

afterEach(() => {
  cleanup()
})

test('overview displays machines and load without write buttons', async () => {
  render(<DashboardPage />)

  // Technical overview header
  await screen.findByText('System Overview')
  expect(screen.queryByText(/Your next experiment starts here/i)).toBeNull()
  expect(screen.queryByText(/Bring your code/i)).toBeNull()

  // Machine metrics
  expect(screen.getByText('2 / 2')).toBeTruthy()
  expect(screen.getByText('Node node-gpu-1')).toBeTruthy()

  // Hardware allocation
  expect(screen.getAllByText('node-gpu-1').length).toBeGreaterThan(0)
  expect(screen.getAllByText('node-tt-1').length).toBeGreaterThan(0)

  // Check that NO write operations exist on overview
  expect(screen.queryByRole('button', { name: /cancel/i })).toBeNull()
  expect(screen.queryByRole('button', { name: /new job/i })).toBeNull()
  expect(screen.queryByRole('button', { name: /add dataset/i })).toBeNull()
})

test('orders logged-in user jobs first, then everyone elses jobs', async () => {
  render(<DashboardPage />)

  // Alice's job should be in the "Your Active Experiments" section
  await screen.findByText('Alices fine-tuning')
  expect(screen.getByText('Your Active Experiments (1)')).toBeTruthy()
  expect(screen.getByText('Your Job')).toBeTruthy()
  expect(screen.getByText('ℹ️ Waiting in cluster fair queue')).toBeTruthy()

  // Bob's job should be in the "Other Cluster Workloads" section
  expect(screen.getByText('Other Cluster Workloads (1)')).toBeTruthy()
  expect(screen.getByText('Bobs training')).toBeTruthy()

  // Inspecting Alice's job opens read-only details & logs modal
  const inspectButtons = screen.getAllByRole('button', { name: /Details & Logs/i })
  fireEvent.click(inspectButtons[0])

  await screen.findByText(/Job Details: Alices fine-tuning/i)
  await screen.findByText(/Console Logs \(Read-Only\)/i)
  expect(screen.getByText(/Epoch 1\/10 complete/i)).toBeTruthy()

  // Still NO cancel button inside modal
  expect(screen.queryByRole('button', { name: /cancel/i })).toBeNull()
})

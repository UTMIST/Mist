// @vitest-environment jsdom
import { afterEach, beforeEach, expect, test, vi } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { TeamsPage } from './TeamsPage'
import { teamsAPI } from '../api'
import type { Workspace } from '../api'

vi.mock('../api', () => ({
  teamsAPI: {
    list: vi.fn(),
    create: vi.fn(),
    update: vi.fn(),
    enroll: vi.fn(),
    removeMember: vi.fn(),
    grant: vi.fn(),
    revoke: vi.fn(),
  },
}))

let mockUser: { id: string; name: string; email: string; role: string; is_admin?: boolean } | null = null

vi.mock('../auth.tsx', () => ({
  useAccount: () => ({
    user: mockUser,
    isAdmin: Boolean(mockUser?.is_admin || (mockUser && mockUser.role.includes('admin'))),
    enabled: true,
  }),
  isUserAdmin: (u: { role?: string; is_admin?: boolean } | null) =>
    Boolean(u?.is_admin || (u && u.role?.includes('admin'))),
  authClient: {
    admin: {
      listUsers: vi.fn().mockResolvedValue({ data: { users: [] } }),
    },
  },
}))

const mockChoose = vi.fn()
const mockRefresh = vi.fn()

let workspaces: Workspace[]

vi.mock('../teams.tsx', () => ({
  useTeam: () => ({
    selected: 'team-nlp',
    team: workspaces.find((w) => w.id === 'team-nlp') ?? null,
    teams: workspaces,
    workspaces,
    storageBudget: 1500,
    choose: mockChoose,
    refresh: mockRefresh,
  }),
  useWorkspace: () => ({
    selected: 'team-nlp',
    team: workspaces.find((w) => w.id === 'team-nlp') ?? null,
    teams: workspaces,
    workspaces,
    storageBudget: 1500,
    choose: mockChoose,
    refresh: mockRefresh,
  }),
}))

beforeEach(() => {
  vi.resetAllMocks()
  HTMLDialogElement.prototype.showModal = function () {
    this.setAttribute('open', '')
  }
  HTMLDialogElement.prototype.close = function () {
    this.removeAttribute('open')
  }

  workspaces = [
    {
      id: 'team-nlp',
      name: 'UTMIST NLP',
      type: 'team',
      description: 'NLP research group',
      disabled: false,
      policy: {
        cpu: '8',
        memory: '32Gi',
        nvidia: 2,
        tenstorrent: 2,
        concurrent: 2,
        queued: 10,
        storage_gib: 200,
        model_storage_gib: 500,
        runtime_seconds: 86400,
        registries: ['docker.io'],
      },
      members: [{ id: 'alice', name: 'Alice', email: 'alice@mist.local', common_writer: true }],
      grants: [],
    },
    {
      id: 'team-alice-personal',
      name: 'Alice (Personal)',
      type: 'personal',
      description: 'Personal quota for Alice',
      disabled: false,
      policy: {
        cpu: '4',
        memory: '16Gi',
        nvidia: 1,
        tenstorrent: 1,
        concurrent: 1,
        queued: 5,
        storage_gib: 50,
        model_storage_gib: 50,
        runtime_seconds: 14400,
        registries: ['docker.io'],
      },
      members: [{ id: 'alice', name: 'Alice', email: 'alice@mist.local', common_writer: true }],
      grants: [],
    },
    {
      id: 'team-collab',
      name: 'Vision-NLP Collab',
      type: 'collaboration',
      description: 'Joint research project',
      disabled: false,
      policy: {
        cpu: '8',
        memory: '32Gi',
        nvidia: 2,
        tenstorrent: 0,
        concurrent: 2,
        queued: 10,
        storage_gib: 100,
        model_storage_gib: 100,
        runtime_seconds: 43200,
        registries: ['docker.io'],
      },
      members: [{ id: 'alice', name: 'Alice', email: 'alice@mist.local', common_writer: false }],
      grants: [],
    },
  ]

  vi.mocked(teamsAPI.list).mockResolvedValue({
    teams: workspaces,
    storage_budget_gib: 1500,
  })
})

afterEach(() => {
  cleanup()
})

test('renders workspaces with type badges and hides admin controls for non-admin', async () => {
  mockUser = { id: 'alice', name: 'Alice', email: 'alice@mist.local', role: 'user' }
  render(<TeamsPage />)

  // Header and attribution description
  await screen.findByText('Workspaces')
  expect(screen.getByText('Attribution & Quotas')).toBeTruthy()

  // Workspace types badges
  expect(screen.getAllByText(/Team Workspace/).length).toBeGreaterThan(0)
  expect(screen.getAllByText(/Personal Quota/).length).toBeGreaterThan(0)
  expect(screen.getAllByText(/Collaboration/).length).toBeGreaterThan(0)

  // Non-admin should NOT see "New workspace" or "Manage"
  expect(screen.queryByRole('button', { name: /New workspace/i })).toBeNull()
  expect(screen.queryByRole('button', { name: /Manage UTMIST NLP/i })).toBeNull()

  // Selecting a workspace invokes choose
  const selectButtons = screen.getAllByRole('button', { name: /Select workspace/i })
  expect(selectButtons.length).toBeGreaterThan(0)
  fireEvent.click(selectButtons[0])
  expect(mockChoose).toHaveBeenCalled()
})

test('displays admin badge, New workspace button, and allows filtering by type for admin', async () => {
  mockUser = { id: 'admin-user', name: 'Admin', email: 'admin@mist.local', role: 'admin', is_admin: true }
  render(<TeamsPage />)

  await screen.findByText('Workspaces')

  // Admin banner & New workspace button
  expect(screen.getByText(/Admin Management Enabled/i)).toBeTruthy()
  const newButton = screen.getByRole('button', { name: /New workspace/i })
  expect(newButton).toBeTruthy()

  // Manage buttons visible for admin
  expect(screen.getByRole('button', { name: /Manage UTMIST NLP/i })).toBeTruthy()

  // Test filter tabs: click Personal Quotas
  fireEvent.click(screen.getByRole('button', { name: /Personal Quotas/i }))
  expect(screen.getByText('Alice (Personal)')).toBeTruthy()
  expect(screen.queryByText('UTMIST NLP')).toBeNull()

  // Click Teams tab
  fireEvent.click(screen.getByRole('button', { name: /Teams/i }))
  expect(screen.getByText('UTMIST NLP')).toBeTruthy()
  expect(screen.queryByText('Alice (Personal)')).toBeNull()

  // Open New workspace modal
  fireEvent.click(newButton)
  await screen.findByText('Create Compute Workspace')
  expect(screen.getByLabelText(/Workspace Type/i)).toBeTruthy()
})

// @vitest-environment jsdom
import { afterEach, beforeEach, expect, test, vi } from 'vitest'
import { cleanup, render, screen } from '@testing-library/react'
import { DatasetsPage } from './DatasetsPage'
import { storageAPI } from '../api'

vi.mock('../api', () => ({ storageAPI: { list: vi.fn(), info: vi.fn() } }))
vi.mock('../teams', () => ({
  useTeam: () => ({
    selected: 'team-a',
    team: { name: 'Team A', members: [] },
  }),
}))
vi.mock('../auth', () => ({
  useAccount: () => ({ user: { id: 'alice', role: 'user' } }),
}))
vi.mock('./TeamStorageBrowser', () => ({ TeamStorageBrowser: () => null }))

beforeEach(() => {
  vi.resetAllMocks()
  vi.mocked(storageAPI.list).mockResolvedValue({
    datasets: [],
    upload_limit_bytes: 1024,
  })
  vi.mocked(storageAPI.info).mockResolvedValue({
    enabled: true,
    capacity_bytes: 100 * 1024 ** 3,
    available_bytes: 60 * 1024 ** 3,
    upload_limit_bytes: 1024,
    storage_mode: 'shared-folders',
    storage_limits_enforced: false,
    dataset_used_bytes: 2 * 1024 ** 3,
    model_used_bytes: 3 * 1024 ** 3,
    dataset_allocation_gib: 1,
    model_allocation_gib: 5,
    usage_stale: true,
    storage_low: true,
  })
})
afterEach(cleanup)

test('shows team file usage independently of shared disk usage and advisory allowances', async () => {
  render(<DatasetsPage />)
  await screen.findByText(/Shared disk:/)
  expect(screen.getByText('2 GiB / 1 GiB')).toBeTruthy()
  expect(screen.getByText('3 GiB / 5 GiB')).toBeTruthy()
  expect(screen.getByText(/Above advisory allowance/)).toBeTruthy()
  expect(screen.getByText(/shared disk space is not reserved/)).toBeTruthy()
  expect(screen.getByText(/Shared disk:/).textContent).toContain(
    '60 GiB free of 100 GiB',
  )
})

test('reports stale usage and shared-disk exhaustion risk separately', async () => {
  render(<DatasetsPage />)
  await screen.findByText(/Storage usage reporting is delayed/)
  expect(screen.getByText(/Shared disk space is low/)).toBeTruthy()
})

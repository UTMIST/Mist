// @vitest-environment jsdom
import { afterEach, beforeEach, expect, test, vi } from 'vitest'
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { JobsPage } from './JobsPage'
import { jobsAPI } from '../api'
import type { Job } from '../api'

vi.mock('../api', () => ({ jobsAPI: { list: vi.fn(), submit: vi.fn(), cancel: vi.fn(), logs: vi.fn() } }))

let jobs: Job[]
beforeEach(() => {
  vi.resetAllMocks()
  jobs = [{ id: 'real-job', name: 'Actual training', created: '2026-10-03T20:00:00Z', job_state: 'Scheduled', accelerator: 'tenstorrent', device_count: 2, cpu: '4', memory: '8Gi', image: 'tt-runtime', checkpoint_directory: '/checkpoints/real-job', message: 'Waiting for boards' }]
  vi.mocked(jobsAPI.list).mockImplementation(async () => ({ jobs, count: jobs.length }))
  vi.mocked(jobsAPI.logs).mockResolvedValue({ logs: 'REAL_TRAINING_OUTPUT' })
  vi.mocked(jobsAPI.submit).mockResolvedValue({ job_id: 'submitted', job: jobs[0] })
  vi.mocked(jobsAPI.cancel).mockImplementation(async () => { jobs = [{ ...jobs[0], job_state: 'Cancelled' }]; return jobs[0] })
})
afterEach(() => { cleanup(); vi.useRealTimers() })

test('shows real job state, reads logs, and cancels through the API', async () => {
  render(<JobsPage />)
  await screen.findByText('Actual training')
  expect(screen.getByText('Waiting for boards')).toBeTruthy()
  fireEvent.click(screen.getByRole('button', { name: 'Logs' }))
  await screen.findByText('REAL_TRAINING_OUTPUT')
  fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
  await screen.findByText('Cancelled')
  expect(jobsAPI.cancel).toHaveBeenCalledWith('real-job')
  expect(screen.queryByRole('button', { name: 'Cancel' })).toBeNull()
})

test('submits the requested Tenstorrent board count', async () => {
  render(<JobsPage />)
  fireEvent.change(screen.getByLabelText('Compute'), { target: { value: 'tenstorrent' } })
  fireEvent.change(screen.getByLabelText('Boards (two chips each)'), { target: { value: '2' } })
  fireEvent.change(screen.getByLabelText('Workload'), { target: { value: 'training-smoke' } })
  fireEvent.click(screen.getByRole('button', { name: 'Submit job' }))
  await waitFor(() => expect(jobsAPI.submit).toHaveBeenCalledWith({ name: '', type: 'training-smoke', accelerator: 'tenstorrent', device_count: 2 }))
})

test('recovers the job list after a temporary API outage', async () => {
  vi.useFakeTimers()
  vi.mocked(jobsAPI.list).mockRejectedValueOnce(new Error('Temporary API outage'))
  render(<JobsPage />)
  await act(async () => {})
  expect(screen.getByRole('alert').textContent).toContain('Temporary API outage')
  await act(async () => { await vi.advanceTimersByTimeAsync(3000) })
  expect(screen.queryByRole('alert')).toBeNull()
  expect(screen.getByText('Actual training')).toBeTruthy()
})

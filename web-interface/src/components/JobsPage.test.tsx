// @vitest-environment jsdom
import { afterEach, beforeEach, expect, test, vi } from 'vitest'
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from '@testing-library/react'
import { JobsPage } from './JobsPage'
import { jobsAPI, storageAPI } from '../api'
import type { Job } from '../api'

vi.mock('../api', () => ({
  storageAPI: { list: vi.fn(), files: vi.fn() },
  jobsAPI: {
    list: vi.fn(),
    submit: vi.fn(),
    cancel: vi.fn(),
    logs: vi.fn(),
    hardware: vi.fn(),
    images: vi.fn(),
  },
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
  vi.mocked(storageAPI.list).mockResolvedValue({
    datasets: [],
    upload_limit_bytes: 2147483648,
  })
  jobs = [
    {
      id: 'real-job',
      name: 'Actual training',
      created: '2026-10-03T20:00:00Z',
      job_state: 'Scheduled',
      accelerator: 'tenstorrent',
      device_count: 2,
      cpu: '4',
      memory: '8Gi',
      image: 'tt-runtime',
      checkpoint_directory: '/checkpoints/real-job',
      message: 'Waiting for boards',
    },
  ]
  vi.mocked(jobsAPI.list).mockImplementation(async () => ({
    jobs,
    count: jobs.length,
  }))
  vi.mocked(jobsAPI.logs).mockResolvedValue({ logs: 'REAL_TRAINING_OUTPUT' })
  vi.mocked(jobsAPI.hardware).mockResolvedValue({
    observed_at: '2026-10-08T12:00:00Z',
    machines: [],
    pools: [
      {
        accelerator: 'nvidia',
        node: 'cpu-node',
        unit: 'GPU',
        total: 2,
        allocated: 1,
        available: 1,
        ready: true,
      },
      {
        accelerator: 'tenstorrent',
        node: 'tt-node',
        unit: 'board',
        total: 4,
        allocated: 2,
        available: 2,
        ready: true,
        chips_per_device: 2,
      },
    ],
  })
  vi.mocked(jobsAPI.images).mockResolvedValue({
    images: [
      { reference: 'cpu-runtime', accelerators: ['cpu'] },
      { reference: 'nvidia-runtime', accelerators: ['nvidia'] },
      { reference: 'tt-runtime', accelerators: ['tenstorrent'] },
    ],
    profiles: [
      {
        accelerator: 'cpu',
        default_image: 'cpu-runtime',
        device_unit: 'none',
        max_devices: 0,
      },
      {
        accelerator: 'nvidia',
        default_image: 'nvidia-runtime',
        device_unit: 'GPU',
        max_devices: 2,
      },
      {
        accelerator: 'tenstorrent',
        default_image: 'tt-runtime',
        device_unit: 'board',
        max_devices: 4,
      },
    ],
  })
  vi.mocked(jobsAPI.submit).mockResolvedValue({
    job_id: 'submitted',
    job: jobs[0],
  })
  vi.mocked(jobsAPI.cancel).mockImplementation(async () => {
    jobs = [{ ...jobs[0], job_state: 'Cancelled' }]
    return jobs[0]
  })
})
afterEach(() => {
  cleanup()
  vi.useRealTimers()
})

test('shows real job state, reads logs, and cancels through the API', async () => {
  render(<JobsPage />)
  await screen.findByText('Actual training')
  fireEvent.click(
    screen.getByRole('button', { name: 'Show details for Actual training' }),
  )
  expect(screen.getByText('Waiting for boards')).toBeTruthy()
  fireEvent.click(screen.getByRole('button', { name: 'Logs' }))
  await screen.findByText('REAL_TRAINING_OUTPUT')
  fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
  await screen.findByText('Cancelled')
  expect(jobsAPI.cancel).toHaveBeenCalledWith('real-job')
  await waitFor(() =>
    expect(screen.queryByRole('button', { name: 'Cancel' })).toBeNull(),
  )
})

test('submits the requested Tenstorrent board count', async () => {
  render(<JobsPage />)
  fireEvent.click(screen.getByRole('button', { name: 'New job' }))
  await screen.findByDisplayValue('cpu-runtime')
  fireEvent.click(screen.getByRole('radio', { name: /Tenstorrent/ }))
  fireEvent.change(screen.getByLabelText('Boards (two chips each)'), {
    target: { value: '2' },
  })
  fireEvent.click(screen.getByRole('radio', { name: 'Training check' }))
  fireEvent.click(screen.getByRole('button', { name: 'Submit job' }))
  await waitFor(() =>
    expect(jobsAPI.submit).toHaveBeenCalledWith({
      name: '',
      type: 'training-smoke',
      accelerator: 'tenstorrent',
      device_count: 2,
      image: 'tt-runtime',
      timeout_seconds: 600,
    }),
  )
})

test('passes executable and each argument separately, preserving spaces', async () => {
  render(<JobsPage />)
  fireEvent.click(screen.getByRole('button', { name: 'New job' }))
  await screen.findByDisplayValue('cpu-runtime')
  fireEvent.click(screen.getByRole('radio', { name: 'Container' }))
  fireEvent.change(screen.getByLabelText('Command executable (optional)'), {
    target: { value: 'python' },
  })
  fireEvent.change(screen.getByLabelText('Arguments (one per line)'), {
    target: { value: "-c\nprint('two words')" },
  })
  fireEvent.change(screen.getByLabelText('Working directory (optional)'), {
    target: { value: '/app' },
  })
  fireEvent.click(screen.getByRole('button', { name: 'Submit job' }))
  await waitFor(() =>
    expect(jobsAPI.submit).toHaveBeenCalledWith(
      expect.objectContaining({
        image: 'cpu-runtime',
        command: ['python'],
        args: ['-c', "print('two words')"],
        working_directory: '/app',
      }),
    ),
  )
})

test('rejects unapproved images and invalid board counts before submission', async () => {
  render(<JobsPage />)
  fireEvent.click(screen.getByRole('button', { name: 'New job' }))
  await screen.findByDisplayValue('cpu-runtime')
  fireEvent.change(screen.getByLabelText('Container image'), {
    target: { value: 'unapproved/image' },
  })
  expect(
    screen.getByRole('button', { name: 'Submit job' }).hasAttribute('disabled'),
  ).toBe(true)
  fireEvent.click(screen.getByRole('radio', { name: /Tenstorrent/ }))
  fireEvent.change(screen.getByLabelText('Boards (two chips each)'), {
    target: { value: '5' },
  })
  expect(
    screen.getByRole('button', { name: 'Submit job' }).hasAttribute('disabled'),
  ).toBe(true)
  expect(jobsAPI.submit).not.toHaveBeenCalled()
})

test('recovers the job list after a temporary API outage', async () => {
  vi.useFakeTimers()
  vi.mocked(jobsAPI.list).mockRejectedValueOnce(
    new Error('Temporary API outage'),
  )
  render(<JobsPage />)
  await act(async () => {})
  expect(screen.getByRole('alert').textContent).toContain(
    'Temporary API outage',
  )
  await act(async () => {
    await vi.advanceTimersByTimeAsync(3000)
  })
  expect(screen.queryByRole('alert')).toBeNull()
  expect(screen.getByText('Actual training')).toBeTruthy()
})

test('paginates history and searches across all pages', async () => {
  jobs = Array.from({ length: 23 }, (_, index) => ({
    ...jobs[0],
    id: `job-${index}`,
    name: `Experiment ${index + 1}`,
  }))
  render(<JobsPage />)
  await screen.findByText('Experiment 1')
  expect(
    screen.getAllByRole('button', { name: /Show details for Experiment/ }),
  ).toHaveLength(10)
  expect(screen.queryByText('Experiment 11')).toBeNull()
  fireEvent.click(screen.getByRole('button', { name: 'Next page' }))
  expect(screen.getByText('Experiment 11')).toBeTruthy()
  expect(screen.queryByText('Experiment 1')).toBeNull()
  fireEvent.click(screen.getByRole('button', { name: 'Next page' }))
  expect(
    screen.getAllByRole('button', { name: /Show details for Experiment/ }),
  ).toHaveLength(3)
  expect(
    screen.getByRole('button', { name: 'Next page' }).hasAttribute('disabled'),
  ).toBe(true)
  fireEvent.change(screen.getByLabelText('Search job history'), {
    target: { value: 'job-22' },
  })
  expect(screen.getByText('Experiment 23')).toBeTruthy()
  expect(
    screen.getAllByRole('button', { name: /Show details for Experiment/ }),
  ).toHaveLength(1)
  expect(
    screen
      .getByRole('button', { name: 'Previous page' })
      .hasAttribute('disabled'),
  ).toBe(true)
})

test('keeps a cleared image blank while editing and uses the image startup defaults', async () => {
  render(<JobsPage />)
  fireEvent.click(screen.getByRole('button', { name: 'New job' }))
  await screen.findByDisplayValue('cpu-runtime')
  fireEvent.click(screen.getByRole('radio', { name: 'Container' }))
  const image = screen.getByLabelText<HTMLInputElement>('Container image')
  fireEvent.change(image, { target: { value: '' } })
  expect(image.value).toBe('')
  expect(screen.getByRole('button', { name: 'Submit job' }).hasAttribute('disabled')).toBe(true)
  fireEvent.change(image, { target: { value: 'cpu' } })
  expect(image.value).toBe('cpu')
  fireEvent.change(image, { target: { value: 'cpu-runtime' } })
  fireEvent.click(screen.getByRole('button', { name: 'Submit job' }))
  await waitFor(() => expect(jobsAPI.submit).toHaveBeenCalled())
  const submission = vi.mocked(jobsAPI.submit).mock.calls[0][0]
  expect(submission.image).toBe('cpu-runtime')
  expect(submission.command).toBeUndefined()
  expect(submission.args).toBeUndefined()
  expect(submission.script).toBeUndefined()
})

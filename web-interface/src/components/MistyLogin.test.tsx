// @vitest-environment jsdom
import { afterEach, expect, test, vi } from 'vitest'
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from '@testing-library/react'
import { MistyLogin } from './MistyLogin'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  vi.useRealTimers()
})

test('requests a code, shows an invalid-code error, then refreshes after verification', async () => {
  const fetchMock = vi
    .fn()
    .mockResolvedValueOnce({
      ok: true,
      json: async () => ({ challenge_id: 'challenge' }),
    })
    .mockResolvedValueOnce({
      ok: false,
      json: async () => ({ message: 'Invalid or expired code' }),
    })
    .mockResolvedValueOnce({
      ok: true,
      json: async () => ({ user: { id: 'existing' } }),
    })
  vi.stubGlobal('fetch', fetchMock)
  const refresh = vi.fn()
  render(<MistyLogin refresh={refresh} />)
  fireEvent.change(screen.getByLabelText('Email'), {
    target: { value: 'member@example.com' },
  })
  fireEvent.click(screen.getByRole('button', { name: 'Send code' }))
  const code = await screen.findByLabelText('Verification code')
  expect(screen.getByRole('status').textContent).toContain(
    'active Misty account',
  )
  expect(
    screen.getByRole('button', { name: /Resend in/ }).hasAttribute('disabled'),
  ).toBe(true)
  fireEvent.change(code, { target: { value: '000000' } })
  fireEvent.click(screen.getByRole('button', { name: 'Verify and sign in' }))
  expect((await screen.findByRole('alert')).textContent).toContain(
    'Invalid or expired code',
  )
  expect(refresh).not.toHaveBeenCalled()
  fireEvent.change(code, { target: { value: '123456' } })
  fireEvent.click(screen.getByRole('button', { name: 'Verify and sign in' }))
  await waitFor(() => expect(refresh).toHaveBeenCalledOnce())
  expect(JSON.parse(fetchMock.mock.calls[2][1].body)).toEqual({
    challenge_id: 'challenge',
    code: '123456',
  })
  expect(fetchMock.mock.calls[2][1].credentials).toBe('same-origin')
})

test('a failed send stays on the email form and permits retry', async () => {
  vi.stubGlobal(
    'fetch',
    vi.fn().mockRejectedValue(new Error('Service unavailable')),
  )
  render(<MistyLogin refresh={() => {}} />)
  fireEvent.change(screen.getByLabelText('Email'), {
    target: { value: 'member@example.com' },
  })
  fireEvent.click(screen.getByRole('button', { name: 'Send code' }))
  expect((await screen.findByRole('alert')).textContent).toContain(
    'Service unavailable',
  )
  expect(screen.queryByLabelText('Verification code')).toBeNull()
  expect(
    screen.getByRole('button', { name: 'Send code' }).hasAttribute('disabled'),
  ).toBe(false)
})

test('retains the previous request when a replacement code has not arrived', async () => {
  const fetchMock = vi
    .fn()
    .mockResolvedValueOnce({
      ok: true,
      json: async () => ({ challenge_id: 'old-request' }),
    })
    .mockResolvedValueOnce({
      ok: true,
      json: async () => ({ challenge_id: 'replacement' }),
    })
    .mockResolvedValueOnce({
      ok: true,
      json: async () => ({ user: { id: 'existing' } }),
    })
  vi.stubGlobal('fetch', fetchMock)
  vi.useFakeTimers()
  const refresh = vi.fn()
  render(<MistyLogin refresh={refresh} />)
  fireEvent.change(screen.getByLabelText('Email'), {
    target: { value: 'member@example.com' },
  })
  await act(async () => {
    fireEvent.click(screen.getByRole('button', { name: 'Send code' }))
  })
  for (let tick = 0; tick < 60; tick++) {
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1000)
    })
  }
  await act(async () => {
    fireEvent.click(screen.getByRole('button', { name: 'Resend code' }))
  })
  fireEvent.change(screen.getByLabelText('Code request'), {
    target: { value: 'old-request' },
  })
  fireEvent.change(screen.getByLabelText('Verification code'), {
    target: { value: '123456' },
  })
  await act(async () => {
    fireEvent.click(screen.getByRole('button', { name: 'Verify and sign in' }))
  })
  expect(JSON.parse(fetchMock.mock.calls[2][1].body)).toEqual({
    challenge_id: 'old-request',
    code: '123456',
  })
  expect(refresh).toHaveBeenCalledOnce()
})

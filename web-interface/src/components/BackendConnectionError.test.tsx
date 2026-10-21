// @vitest-environment jsdom
import { afterEach, expect, test, vi } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { BackendConnectionError } from './BackendConnectionError'

afterEach(cleanup)

test('renders connection error message and status badge', () => {
  render(
    <BackendConnectionError error="Failed to fetch /api/session: connection refused" />,
  )

  const heading = screen.getByRole('heading', { level: 1 })
  expect(heading.textContent).toContain('Unable to reach the compute backend')
  expect(
    screen.getByText('Failed to fetch /api/session: connection refused'),
  ).toBeTruthy()
  expect(screen.getByText('Backend Offline')).toBeTruthy()
})

test('triggers onRetry when Try again button is clicked', () => {
  const onRetry = vi.fn()
  render(
    <BackendConnectionError
      error="Account service unavailable"
      onRetry={onRetry}
    />,
  )

  const retryButton = screen.getByRole('button', {
    name: 'Try again to connect to the backend',
  })
  expect(retryButton).toBeTruthy()
  expect(retryButton.textContent).toContain('Try again')

  fireEvent.click(retryButton)
  expect(onRetry).toHaveBeenCalledTimes(1)
})

test('disables button and displays loading state when isRetrying is true', () => {
  render(
    <BackendConnectionError
      error="Account service unavailable"
      onRetry={() => {}}
      isRetrying={true}
    />,
  )

  const retryButton = screen.getByRole('button', {
    name: 'Try again to connect to the backend',
  })
  expect(retryButton.hasAttribute('disabled')).toBe(true)
  expect(retryButton.textContent).toContain('Checking connection…')
})

test('provides links to static documentation and GitHub issues', () => {
  render(
    <BackendConnectionError
      error="Account service unavailable"
      onRetry={() => {}}
    />,
  )

  const docsLink = screen.getByRole('link', {
    name: /Documentation.*View documentation/s,
  })
  expect(docsLink.getAttribute('href')).toBe('/docs/')

  const githubLink = screen.getByRole('link', {
    name: /Report an Issue.*Open GitHub issues/s,
  })
  expect(githubLink.getAttribute('href')).toBe(
    'https://github.com/UTMIST/Mist/issues',
  )
  expect(githubLink.getAttribute('target')).toBe('_blank')
})

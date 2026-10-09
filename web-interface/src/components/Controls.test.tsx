// @vitest-environment jsdom
import { afterEach, expect, test, vi } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { useState } from 'react'
import { Picker } from './Picker'
import { Tabs } from './Tabs'

afterEach(cleanup)

test('picker filters a long account list and selects by keyboard', () => {
  const change = vi.fn()
  render(
    <Picker aria-label="Account" value="" onChange={change}>
      {Array.from({ length: 30 }, (_, i) => (
        <option key={i} value={i}>
          Researcher {i}
        </option>
      ))}
    </Picker>,
  )
  const trigger = screen.getByRole('combobox', { name: 'Account' })
  fireEvent.click(trigger)
  const search = screen.getByRole('combobox', { name: 'Search Account' })
  expect(document.activeElement).toBe(search)
  fireEvent.change(search, { target: { value: 'Researcher 29' } })
  expect(screen.getAllByRole('option')).toHaveLength(1)
  fireEvent.keyDown(search, { key: 'Enter' })
  expect(change).toHaveBeenCalledWith({ target: { value: '29' } })
  expect(screen.queryByRole('listbox')).toBeNull()
  expect(document.activeElement).toBe(trigger)
})

test('empty search cannot select an unrelated option; Escape returns focus', () => {
  const change = vi.fn()
  render(
    <Picker aria-label="Device" value="cpu" onChange={change}>
      <option value="cpu">CPU</option>
    </Picker>,
  )
  const trigger = screen.getByRole('combobox', { name: 'Device' })
  fireEvent.click(trigger)
  const search = screen.getByRole('combobox', { name: 'Search Device' })
  fireEvent.change(search, { target: { value: 'missing' } })
  fireEvent.keyDown(search, { key: 'ArrowDown' })
  fireEvent.keyDown(search, { key: 'Enter' })
  expect(change).not.toHaveBeenCalled()
  expect(screen.getByText('No matches')).toBeTruthy()
  fireEvent.keyDown(search, { key: 'Escape' })
  expect(screen.queryByRole('listbox')).toBeNull()
  expect(document.activeElement).toBe(trigger)
})

test('tab keyboard navigation wraps, focuses the chosen tab and controls the panel', () => {
  function Settings() {
    const [tab, setTab] = useState('members')
    return (
      <>
        <Tabs
          id="settings"
          label="Settings"
          value={tab}
          onChange={setTab}
          items={[
            ['members', 'Members'],
            ['limits', 'Limits'],
          ]}
        />
        <div id="settings-limits-panel" hidden={tab !== 'limits'}>
          Resource limits
        </div>
      </>
    )
  }
  render(<Settings />)
  fireEvent.keyDown(screen.getByRole('tab', { name: 'Members' }), {
    key: 'ArrowLeft',
  })
  const limits = screen.getByRole('tab', { name: 'Limits' })
  expect(limits.getAttribute('aria-selected')).toBe('true')
  expect(document.activeElement).toBe(limits)
  expect(
    document.getElementById(limits.getAttribute('aria-controls')!)?.hidden,
  ).toBe(false)
  fireEvent.keyDown(limits, { key: 'Home' })
  expect(
    screen.getByRole('tab', { name: 'Members' }).getAttribute('tabindex'),
  ).toBe('0')
})

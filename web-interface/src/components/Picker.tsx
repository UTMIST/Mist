import {
  Children,
  isValidElement,
  useEffect,
  useId,
  useRef,
  useState,
} from 'react'
import { Check, ChevronDown, Search } from 'lucide-react'
import type { ReactNode } from 'react'

type Props = {
  id?: string
  value: string | number
  onChange: (event: { target: { value: string } }) => void
  children: ReactNode
  disabled?: boolean
  required?: boolean
  className?: string
  'aria-label'?: string
}
function text(node: ReactNode): string {
  return Children.toArray(node)
    .map((child) =>
      isValidElement<{ children?: ReactNode }>(child)
        ? text(child.props.children)
        : String(child),
    )
    .join('')
}

/** Searchable, keyboard-operable single selection shared across the portal. */
export function Picker({
  id,
  value,
  onChange,
  children,
  disabled,
  required,
  className = '',
  'aria-label': ariaLabel,
}: Props) {
  const options = Children.toArray(children)
    .filter(isValidElement<{ value: string | number; children: ReactNode }>)
    .map((option) => ({
      value: String(option.props.value),
      label: text(option.props.children),
    }))
  const selected = options.find((option) => option.value === String(value))
  const [open, setOpen] = useState(false)
  const [above, setAbove] = useState(false)
  const [query, setQuery] = useState('')
  const [active, setActive] = useState(0)
  const root = useRef<HTMLDivElement>(null)
  const trigger = useRef<HTMLButtonElement>(null)
  const search = useRef<HTMLInputElement>(null)
  const uniqueId = useId()
  const filtered = options.filter((option) =>
    option.label.toLowerCase().includes(query.toLowerCase()),
  )
  const listId = `${id ?? uniqueId}-options`
  useEffect(() => {
    if (!open) return
    const list = document.getElementById(listId)
    const option = document.getElementById(`${listId}-${active}`)
    if (!list || !option) return
    const bounds = list.getBoundingClientRect()
    const item = option.getBoundingClientRect()
    if (item.top < bounds.top) list.scrollTop -= bounds.top - item.top
    else if (item.bottom > bounds.bottom)
      list.scrollTop += item.bottom - bounds.bottom
  }, [open, active, listId, query])
  useEffect(() => {
    if (!open) return
    function place() {
      const rect = root.current?.getBoundingClientRect()
      if (!rect) return
      const bound = Math.min(
        window.innerHeight,
        root.current?.closest('dialog')?.getBoundingClientRect().bottom ??
          window.innerHeight,
      )
      setAbove(bound - rect.bottom < 260 && rect.top > bound - rect.bottom)
    }
    place()
    search.current?.focus()
    function outside(event: PointerEvent) {
      if (!root.current?.contains(event.target as Node)) setOpen(false)
    }
    document.addEventListener('pointerdown', outside)
    const outsideFocus = (event: FocusEvent) => {
      if (!root.current?.contains(event.target as Node)) setOpen(false)
    }
    document.addEventListener('focusin', outsideFocus)
    window.addEventListener('resize', place)
    document.addEventListener('scroll', place, true)
    return () => {
      document.removeEventListener('pointerdown', outside)
      document.removeEventListener('focusin', outsideFocus)
      window.removeEventListener('resize', place)
      document.removeEventListener('scroll', place, true)
    }
  }, [open])
  function choose(next: string) {
    onChange({ target: { value: next } })
    setOpen(false)
    trigger.current?.focus()
  }
  return (
    <div
      className={`mist-picker ${above ? 'is-above' : ''} ${className}`}
      ref={root}
    >
      <button
        type="button"
        id={id}
        ref={trigger}
        role="combobox"
        aria-label={ariaLabel}
        aria-haspopup="listbox"
        aria-expanded={open}
        aria-controls={listId}
        aria-required={required}
        disabled={disabled}
        className="mist-picker-trigger"
        onClick={() => {
          setOpen(!open)
          setQuery('')
          setActive(0)
        }}
        onKeyDown={(event) => {
          if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
            event.preventDefault()
            setOpen(true)
          }
        }}
      >
        <span>{selected?.label ?? 'Select…'}</span>
        <ChevronDown size={15} aria-hidden="true" />
      </button>
      {open && (
        <div
          className="mist-picker-menu"
          onKeyDown={(event) => {
            if (event.key === 'Escape') {
              event.preventDefault()
              event.stopPropagation()
              setOpen(false)
              trigger.current?.focus()
            }
            if (event.key === 'ArrowDown') {
              event.preventDefault()
              setActive((index) =>
                Math.max(0, Math.min(index + 1, filtered.length - 1)),
              )
            }
            if (event.key === 'ArrowUp') {
              event.preventDefault()
              setActive((index) => Math.max(index - 1, 0))
            }
            if (event.key === 'Enter' && filtered[active]) {
              event.preventDefault()
              choose(filtered[active].value)
            }
          }}
        >
          <div className="mist-picker-search">
            <Search size={14} aria-hidden="true" />
            <input
              ref={search}
              role="combobox"
              aria-expanded="true"
              aria-autocomplete="list"
              aria-label={ariaLabel ? `Search ${ariaLabel}` : 'Search options'}
              placeholder="Type to find…"
              value={query}
              onChange={(event) => {
                setQuery(event.target.value)
                setActive(0)
              }}
              aria-controls={listId}
              aria-activedescendant={
                filtered[active] ? `${listId}-${active}` : undefined
              }
            />
          </div>
          <div role="listbox" id={listId} aria-label={ariaLabel ?? 'Options'}>
            {filtered.map((option, index) => (
              <button
                type="button"
                role="option"
                tabIndex={-1}
                id={`${listId}-${index}`}
                aria-selected={String(value) === option.value}
                key={option.value}
                data-value={option.value}
                className={active === index ? 'is-highlighted' : ''}
                onPointerMove={() => setActive(index)}
                onClick={() => choose(option.value)}
              >
                <span>{option.label}</span>
                {String(value) === option.value && (
                  <Check size={14} aria-hidden="true" />
                )}
              </button>
            ))}
            {!filtered.length && (
              <p className="mist-picker-empty">No matches</p>
            )}
          </div>
        </div>
      )}
    </div>
  )
}

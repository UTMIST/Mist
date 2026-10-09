/** Shared tab navigation, with arrow keys, Home/End and a single tab stop. */
export function Tabs({
  id,
  label,
  value,
  onChange,
  items,
}: {
  id: string
  label: string
  value: string
  onChange: (value: string) => void
  items: readonly (readonly [string, string])[]
}) {
  return (
    <div className="mist-settings-tabs" role="tablist" aria-label={label}>
      {items.map(([key, title], index) => (
        <button
          key={key}
          id={`${id}-${key}-tab`}
          type="button"
          role="tab"
          tabIndex={value === key ? 0 : -1}
          aria-selected={value === key}
          aria-controls={`${id}-${key}-panel`}
          onClick={() => onChange(key)}
          onKeyDown={(event) => {
            const next =
              event.key === 'ArrowRight'
                ? (index + 1) % items.length
                : event.key === 'ArrowLeft'
                  ? (index + items.length - 1) % items.length
                  : event.key === 'Home'
                    ? 0
                    : event.key === 'End'
                      ? items.length - 1
                      : null
            if (next === null) return
            event.preventDefault()
            onChange(items[next][0])
            document.getElementById(`${id}-${items[next][0]}-tab`)?.focus()
          }}
        >
          {title}
        </button>
      ))}
    </div>
  )
}

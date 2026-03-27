export function ButtonSuccess({ onClick, text } : { onClick: () => void, text: string }) {
  return (
    <button
      onClick={onClick}
      className="px-3 py-1 text-xs font-semibold rounded bg-green-200 text-green-800 hover:bg-green-300"
    >
      { text }
    </button>
  )
}

export function ButtonWarning({ onClick, text } : { onClick: () => void, text: string }) {
  return (
    <button
      onClick={onClick}
      className="px-3 py-1 text-xs font-semibold rounded bg-yellow-200 text-yellow-800 hover:bg-yellow-300"
    >
      { text }
    </button>
  )
}

export function ButtonDanger({ onClick, text } : { onClick: () => void, text: string }) {
  return (
    <button
      onClick={onClick}
      className="px-3 py-1 text-xs font-semibold rounded bg-red-200 text-red-800 hover:bg-red-300"
    >
      { text }
    </button>
  )
}
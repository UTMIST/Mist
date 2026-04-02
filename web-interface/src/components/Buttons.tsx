const variantStyles = {
  success: 'bg-green-200 text-green-800 hover:bg-green-300',
  warning: 'bg-yellow-200 text-yellow-800 hover:bg-yellow-300',
  danger: 'bg-red-200 text-red-800 hover:bg-red-300',
}

type ButtonVariant = keyof typeof variantStyles

export function Button({
  onClick,
  text,
  variant,
}: {
  onClick: () => void
  text: string
  variant: ButtonVariant
}) {
  return (
    <button
      onClick={onClick}
      className={`px-3 py-1 text-xs font-semibold rounded ${variantStyles[variant]}`}
    >
      {text}
    </button>
  )
}

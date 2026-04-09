import type { ReactNode } from 'react'

const variantStyles = {
  success: 'bg-green-200 text-green-800 hover:bg-green-300',
  warning: 'bg-yellow-200 text-yellow-800 hover:bg-yellow-300',
  danger: 'bg-red-200 text-red-800 hover:bg-red-300',
  normal: 'bg-blue-200 text-blue-800 hover:bg-blue-300',
  disabled: 'bg-gray-200 text-gray-800',
}

const fontSizeStyles = {
  xs: 'text-xs',
  sm: 'text-sm',
  base: 'text-base',
  lg: 'text-lg',
  xl: 'text-xl',
};


type ButtonVariant = keyof typeof variantStyles
type ButtonFontSize = keyof typeof fontSizeStyles

export function Button({
  children,
  onClick,
  variant,
  fontSize,
}: {
  children: ReactNode
  onClick: () => void
  variant: ButtonVariant
  fontSize: ButtonFontSize
}) {
  return (
    <button
      onClick={onClick}
      className={`flex items-center gap-1.5 px-3 py-1 ${fontSizeStyles[fontSize]} rounded ${variantStyles[variant]}`}
      disabled={variant === 'disabled'}
    >
      {children}
    </button>
  )
}

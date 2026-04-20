import { Link } from '@tanstack/react-router'
import type { ReactNode } from 'react'

export function CardInfoField({
  label,
  value,
  link,
  hash,
}: {
  label: string
  value: string
  link?: string
  hash?: string
}) {
  return (
    <div>
      <div className="text-xs text-gray-500 font-medium">{label}</div>
      {link ? (
        <Link to={link} hash={hash} className="text-sm text-accent underline">
          {value}
        </Link>
      ) : (
        <div className="text-sm font-medium">{value}</div>
      )}
    </div>
  )
}

const headerStyles = {
  default: '',
  error: 'text-red-500',
}

export function CardHeader({
  header,
  headerStyle,
  children,
}: {
  header: string
  headerStyle: keyof typeof headerStyles
  children: ReactNode
}) {
  return (
    <div className="flex items-center justify-between mb-4">
      <h2 className={`text-lg font-bold ${headerStyles[headerStyle]}`}>
        {header}
      </h2>
      <div className="flex gap-2">
        {/* Actions */}
        {children}
      </div>
    </div>
  )
}

export default function Card({
  children,
  id,
}: {
  children: ReactNode
  id?: string
}) {
  return (
    <div id={id} className="border border-gray-200 rounded-xl p-5">
      {children}
    </div>
  )
}

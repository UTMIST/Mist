import { Link } from '@tanstack/react-router'
import type {ReactNode} from "react";


export function CardInfoField({
                     label,
                     value,
                     link,
                   }: {
  label: string
  value: string
  link?: string
}) {
  return (
    <div>
      <div className="text-xs text-gray-500 font-medium">{label}</div>
      {link ? (
        <Link to={link} className="text-sm text-accent underline">
          {value}
        </Link>
      ) : (
        <div className="text-sm font-medium">{value}</div>
      )}
    </div>
  )
}

export function CardHeader({ header, children }: { header: string, children: ReactNode }) {
  return (
    <div className="flex items-center justify-between mb-4">
      <h2 className="text-lg font-bold">{header}</h2>
      <div className="flex gap-2">
        {/* Actions */}
        { children }
      </div>
    </div>
  )
}

export default function Card({ children }: { children: ReactNode }) {
  return (
    <div className="border border-gray-200 rounded-xl p-5">
      { children }
    </div>
  )
}

import { Link } from '@tanstack/react-router'

export default function Navbar() {
  return (
    <nav className="border-b border-slate-200 bg-white px-5 sm:px-8">
      <div className="mx-auto flex max-w-6xl flex-wrap items-center justify-between gap-4 py-5">
        <Link to="/jobs" className="text-xl font-extrabold tracking-tight">
          MIST{' '}
          <span className="ml-2 text-xs font-medium text-slate-500">
            Local compute
          </span>
        </Link>
        <div className="flex gap-2 text-sm font-semibold">
          {(
            [
              ['/dashboard', 'Overview'],
              ['/machines', 'Machine'],
              ['/jobs', 'Jobs'],
            ] as const
          ).map(([to, title]) => (
            <Link
              key={to}
              to={to}
              className="rounded-lg px-4 py-2 hover:bg-slate-100"
              activeProps={{ className: 'bg-blue-50 text-blue-700' }}
              inactiveProps={{ className: 'text-slate-500' }}
            >
              {title}
            </Link>
          ))}
        </div>
      </div>
    </nav>
  )
}

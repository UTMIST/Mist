import { Link } from '@tanstack/react-router'
import { authClient, useAccount } from '#/auth.tsx'
import { useState } from 'react'
import { LayoutDashboard, Play, Database, Server, Users } from 'lucide-react'
import { WorkspaceSelector } from '#/teams.tsx'

export default function Navbar() {
  const account = useAccount()
  const [error, setError] = useState('')
  return (
    <header className="mist-sidebar">
      <Link to="/dashboard" className="mist-brand" aria-label="Mist overview">
        <svg
          width="28"
          height="28"
          viewBox="0 0 28 28"
          fill="none"
          aria-hidden="true"
        >
          <path d="M14 2 26 9 14 16 2 9 14 2Z" fill="currentColor" />
          <path
            d="m2 14 12 7 12-7M2 19l12 7 12-7"
            stroke="currentColor"
            strokeWidth="2"
          />
        </svg>
        <span>
          Mist<span className="mist-brand-caption">Research compute</span>
        </span>
      </Link>
      <p className="mist-nav-caption">WORKSPACE</p>
      <nav aria-label="Main navigation" className="mist-nav">
        <Link to="/dashboard" activeProps={{ className: 'is-active' }}>
          <span aria-hidden="true">
            <LayoutDashboard size={17} />
          </span>{' '}
          Overview
        </Link>
        <Link to="/jobs" activeProps={{ className: 'is-active' }}>
          <span aria-hidden="true">
            <Play size={17} />
          </span>{' '}
          Jobs
        </Link>
        <Link to="/datasets" activeProps={{ className: 'is-active' }}>
          <span aria-hidden="true">
            <Database size={17} />
          </span>{' '}
          Datasets
        </Link>
        <Link to="/machines" activeProps={{ className: 'is-active' }}>
          <span aria-hidden="true">
            <Server size={17} />
          </span>{' '}
          Machines
        </Link>
        <Link to="/teams" activeProps={{ className: 'is-active' }}>
          <span aria-hidden="true">
            <Users size={17} />
          </span>{' '}
          Teams
        </Link>
      </nav>
      <div className="mist-sidebar-account">
        <WorkspaceSelector />
        <Link
          to="/profile"
          activeProps={{ className: 'is-active' }}
          className="mist-account-link"
        >
          <span className="mist-avatar" aria-hidden="true">
            {account.user?.name[0] ?? 'M'}
          </span>
          <span>
            {account.user?.name ?? 'Local pilot'}
            <span className="mist-brand-caption">Account settings</span>
          </span>
        </Link>
        {account.enabled && (
          <button
            onClick={async () => {
              const result = await authClient.signOut()
              if (result.error)
                setError(result.error.message ?? 'Sign out failed')
              else account.refresh()
            }}
          >
            Sign out
          </button>
        )}
        {error && <p role="alert">{error}</p>}
      </div>
    </header>
  )
}

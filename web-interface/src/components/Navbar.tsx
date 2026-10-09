import { Link } from '@tanstack/react-router'
import { authClient, useAccount } from '#/auth.tsx'
import { useState } from 'react'

export default function Navbar() {
  const account = useAccount()
  const [error, setError] = useState('')
  return (
    <header className="flex flex-wrap items-center justify-between gap-4 border-b px-6 py-4 lg:px-16">
      <nav aria-label="Main navigation" className="flex gap-6">
        <Link to="/dashboard">Mist</Link>
        <Link to="/machines">Machines</Link>
        <Link to="/jobs">Jobs</Link>
        <Link to="/datasets">Datasets</Link>
      </nav>
      <div className="flex gap-4 items-center">
        <Link to="/profile">{account.user?.name ?? 'Local pilot'}</Link>
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

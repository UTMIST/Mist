import { createFileRoute, Link  } from '@tanstack/react-router'
import { useAccount } from '#/auth.tsx'

export const Route = createFileRoute('/dashboard')({ component: Dashboard })
function Dashboard() {
  const account = useAccount()
  return (
    <main className="px-6 py-8 lg:px-16 space-y-4">
      <h1 className="text-2xl font-bold">
        Welcome{account.user ? `, ${account.user.name}` : ''}
      </h1>
      <p>
        Upload a dataset, choose an approved container image and compute device,
        then run a job. Saved outputs can be downloaded after it finishes.
      </p>
      <div className="flex gap-6">
        <Link to="/datasets">Upload a dataset</Link>
        <Link to="/jobs">Run a job</Link>
        <Link to="/machines">View compute</Link>
      </div>
    </main>
  )
}

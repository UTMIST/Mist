import { createFileRoute, Link } from '@tanstack/react-router'
import { jobsAPI, storageAPI } from '#/api.ts'
import { useAccount } from '#/auth.tsx'
import { useTeam } from '#/teams.tsx'
import { usePolling } from '#/hooks/usePolling.ts'
import { HardwarePanel } from '#/components/HardwarePanel.tsx'

export const Route = createFileRoute('/dashboard')({ component: Dashboard })
function Dashboard() {
  const { user } = useAccount()
  const { team } = useTeam()
  const hardware = usePolling(jobsAPI.hardware, 10000)
  const jobs = usePolling(jobsAPI.list, 10000)
  const datasets = usePolling(storageAPI.list, 15000)
  return (
    <main className="space-y-8">
      <div className="mist-page-heading">
        <div>
          <p className="mist-eyebrow">{team?.name ?? 'Research workspace'}</p>
          <h1>Overview</h1>
        </div>
        <span className="mist-live">
          <span />
          Private research portal
        </span>
      </div>
      <section className="mist-overview-hero">
        <p className="mist-eyebrow">
          WELCOME BACK{user ? `, ${user.name.toUpperCase()}` : ''}
        </p>
        <h2>
          Your next experiment
          <br />
          starts here.
        </h2>
        <p>
          One workspace for your data, your team
          <br className="hidden md:block" /> and the compute that brings it
          together.
        </p>
        <div className="flex flex-wrap gap-3 mt-7">
          <Link to="/jobs" className="mist-primary-link">
            Run an experiment <span aria-hidden="true">↗</span>
          </Link>
          <Link to="/datasets" className="mist-secondary-link">
            Upload a dataset <span aria-hidden="true">↗</span>
          </Link>
        </div>
      </section>
      {!team && (
        <div className="mist-card p-5 border border-gray-200">
          <p className="font-medium">Choose your team to get started.</p>
          <p className="mist-description">
            Use the workspace selector or visit Teams. Your previous jobs and
            files remain available in the legacy workspace.
          </p>
          <Link to="/teams" className="inline-block mt-3 text-accent">
            View teams →
          </Link>
        </div>
      )}
      <div className="mist-metrics">
        {[
          [
            'Running experiments',
            jobs.data?.jobs.filter((j) => j.job_state === 'InProgress').length,
          ],
          ['Saved datasets', datasets.data?.datasets.length],
          [
            'Connected machines',
            hardware.data?.machines.filter((m) => m.ready).length,
          ],
        ].map(([label, count]) => (
          <div key={label}>
            <span>{label}</span>
            <strong>{count ?? '—'}</strong>
          </div>
        ))}
      </div>
      {(jobs.error || datasets.error) && (
        <p role="alert" className="text-red-700">
          Workspace unavailable: {jobs.error || datasets.error}
        </p>
      )}
      <HardwarePanel
        hardware={hardware.data}
        error={hardware.error}
        loading={hardware.loading}
      />
      <div className="grid md:grid-cols-3 gap-4">
        {[
          {
            to: '/jobs' as const,
            n: '01',
            title: 'Bring your code',
            copy: 'Choose your container image, hardware and resources. Follow each run from the queue to completion.',
          },
          {
            to: '/datasets' as const,
            n: '02',
            title: 'Keep data together',
            copy: 'Share datasets with teammates and download saved results from your team’s folders.',
          },
          {
            to: '/teams' as const,
            n: '03',
            title: 'Work as a team',
            copy: 'Find your workspace, teammates and the resources available for your research.',
          },
        ].map((item) => (
          <Link key={item.n} to={item.to} className="mist-overview-tile">
            <span className="mist-eyebrow">{item.n}</span>
            <h2>
              {item.title} <span aria-hidden="true">↗</span>
            </h2>
            <p>{item.copy}</p>
          </Link>
        ))}
      </div>
    </main>
  )
}

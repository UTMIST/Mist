import { createFileRoute, Link } from '@tanstack/react-router'
import { HardwarePanel } from '#/components/HardwarePanel'
import { useHardware, useJobs } from '#/localCompute'

export const Route = createFileRoute('/dashboard')({ component: DashboardPage })

function DashboardPage() {
  const { hardware, error } = useHardware()
  const { jobs, error: jobsError } = useJobs()
  return (
    <main className="mx-auto max-w-6xl space-y-7 px-5 py-10 sm:px-8">
      <h1 className="text-3xl font-bold">Compute overview</h1>
      <HardwarePanel hardware={hardware} error={error} />
      <section className="rounded-2xl border border-slate-200 p-6">
        <h2 className="text-lg font-bold">Recent activity</h2>
        {jobsError ? (
          <p role="alert" className="mt-3 text-red-700">
            {jobsError}
          </p>
        ) : (
          <p className="mt-3 text-slate-600">
            {jobs.filter((job) => job.job_state === 'Success').length} completed
            ·{' '}
            {
              jobs.filter((job) =>
                ['Scheduled', 'InProgress'].includes(job.job_state),
              ).length
            }{' '}
            queued or running ·{' '}
            {jobs.filter((job) => job.job_state === 'Failure').length} failed,
            across the latest 50 jobs.
          </p>
        )}
        <Link
          to="/jobs"
          className="mt-6 inline-block rounded-lg bg-blue-600 px-5 py-3 text-sm font-bold text-white"
        >
          Run a GPU benchmark
        </Link>
      </section>
    </main>
  )
}

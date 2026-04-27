import { createFileRoute, Link } from '@tanstack/react-router'
import { generateSampleData } from '#/util.ts'
import type { Alert } from '#/types/alert.ts'
import type { Job } from '#/types/job.ts'
import type { Machine } from '#/types/machine.ts'
import Card, { CardHeader } from '#/components/Card.tsx'
import { format } from 'date-fns'

export const Route = createFileRoute('/dashboard')({
  component: RouteComponent,
  loader: loadDashboardData,
})

type DashboardData = {
  jobs: Job[]
  machines: Machine[]
  alerts: Alert[]
}

function loadDashboardData(): DashboardData {
  const sampleData = generateSampleData()

  return {
    jobs: sampleData.jobs,
    machines: sampleData.machines,
    alerts: sampleData.alerts,
  }
}

function RouteComponent() {
  const loaderData = Route.useLoaderData()

  return (
    <div className="px-16 py-8">
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-10">
        <Card>
          <CardHeader header="Jobs" />
          <table className="w-full">
            <thead>
              <tr className="border-b border-b-gray-400">
                <th className="text-left py-1.5">ID</th>
                <th className="text-left py-1.5">Machine</th>
                <th className="text-left py-1.5">Accessed</th>
              </tr>
            </thead>
            <tbody>
              {loaderData.jobs.map((job) => (
                <tr className="border-b border-b-gray-400">
                  <td className="py-1.5">
                    <Link
                      to="/jobs"
                      hash={job.id}
                      className="text-accent underline"
                    >
                      {job.id}
                    </Link>
                  </td>
                  <td className="py-1.5">
                    <Link
                      to="/machines"
                      hash={job.machine.id}
                      className="text-accent underline"
                    >
                      {job.machine.id}
                    </Link>
                  </td>
                  <td className="py-1.5">
                    {format(job.accessed, 'yyyy-MM-dd')}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </Card>
      </div>
    </div>
  )
}

import { createFileRoute, Link } from '@tanstack/react-router'
import { generateSampleData } from '#/util.ts'
import type { Alert } from '#/types/alert.ts'
import type { Job } from '#/types/job.ts'
import type { Machine } from '#/types/machine.ts'
import Card, { CardHeader } from '#/components/Card.tsx'
import { format } from 'date-fns'
import Chart from '#/components/Chart.tsx'

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

const alertStyles = {
  low: 'bg-green-400',
  medium: 'bg-yellow-400',
  high: 'bg-red-400',
}

function Alert({ alert }: { alert: Alert }) {
  return (
    <div className={`${alertStyles[alert.severity]} border rounded p-2`}>
      {alert.message}
    </div>
  )
}

function RouteComponent() {
  const loaderData = Route.useLoaderData()

  return (
    <div className="px-16 py-8">
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-10">
        <Card>
          <CardHeader header="Jobs" headerStyle="default" />
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
        <Card>
          <CardHeader header="Machines" headerStyle="default" />
          <table className="w-full">
            <thead>
              <tr className="border-b border-b-gray-400">
                <th className="text-left py-1.5">ID</th>
                <th className="text-left py-1.5">Availability</th>
                <th className="text-left py-1.5">Purpose</th>
              </tr>
            </thead>
            <tbody>
              {loaderData.machines.map((machine) => (
                <tr className="border-b border-b-gray-400">
                  <td className="py-1.5">
                    <Link
                      to="/machines"
                      hash={machine.id}
                      className="text-accent underline"
                    >
                      {machine.id}
                    </Link>
                  </td>
                  <td
                    className={`py-1.5 ${machine.isAvailable ? 'text-green-600' : 'text-red-600'}`}
                  >
                    {machine.isAvailable ? 'Available' : 'Not Availabile'}
                  </td>
                  <td className="py-1.5">{machine.purpose}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </Card>
        <Card className="max-h-[35vh] overflow-auto">
          <CardHeader header="GPU" headerStyle="default" />
          {/* TODO: show an actual Grafana chart usage of all machines --> */}
          {/* I did not spend the time to make a multi machine chart because it will be replaced by the actual Granafa graph anyway */}
          <Chart data={loaderData.machines[0].usageHistory} />
        </Card>
        <Card className="flex flex-col max-h-[35vh]">
          <CardHeader header="Alerts" headerStyle="default" />
          <div className="flex flex-col gap-2 overflow-auto">
            {loaderData.alerts.map((alert) => (
              <Alert alert={alert} />
            ))}
          </div>
        </Card>
        <Card className="max-h-[35vh] overflow-auto">
          <CardHeader header="CPU" headerStyle="default" />
          {/* TODO: show an actual Grafana chart usage of all machines --> */}
          {/* I did not spend the time to make a multi machine chart because it will be replaced by the actual Granafa graph anyway */}
          <Chart data={loaderData.machines[0].usageHistory} />
        </Card>
        <Card className="max-h-[35vh] overflow-auto">
          <CardHeader header="RAM" headerStyle="default" />
          {/* TODO: show an actual Grafana chart usage of all machines --> */}
          {/* I did not spend the time to make a multi machine chart because it will be replaced by the actual Granafa graph anyway */}
          <Chart data={loaderData.machines[0].usageHistory} />
        </Card>
      </div>
    </div>
  )
}

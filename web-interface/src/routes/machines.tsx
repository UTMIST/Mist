import { createFileRoute, Link } from '@tanstack/react-router'
import type { Machine } from '#/types/machine.ts'
import Card, { CardHeader, CardInfoField } from '#/components/Card.tsx'
import { Button } from '#/components/Buttons.tsx'
import Chart from '#/components/Chart.tsx'
import { format } from 'date-fns'
import {generateSampleData} from "#/util.ts";

export const Route = createFileRoute('/machines')({
  component: RouteComponent,
  loader: loadMachines,
})

function loadMachines(): Machine[] {
  // TODO: Implement actual API calling. For now we just return sample data
  const data = generateSampleData()

  return data.machines
}

function MachineCard({
  machine,
  onProvision,
}: {
  machine: Machine
  onProvision: (machineId: string) => void
}) {
  return (
    <Card id={machine.id}>
      <CardHeader header={machine.id}>
        <Button
          onClick={() => onProvision(machine.id)}
          variant="normal"
          fontSize="xs"
        >
          Provision
        </Button>
      </CardHeader>

      {/* Info grid */}
      <div className="grid grid-cols-3">
        <div className="grid grid-rows-4 gap-y-3">
          <CardInfoField label="GPU" value={machine.gpu} />
          <CardInfoField label="CPU" value={machine.cpu} />
          <CardInfoField
            label="Number of Jobs"
            value={machine.jobs.length.toString()}
          />
          <CardInfoField label="Disk Usage" value={machine.diskUsage} />
        </div>
        <div className="grid grid-rows-4 gap-y-3">
          <CardInfoField label="CPU Utilization" value={machine.cpuUsage} />
          <CardInfoField label="RAM" value={machine.ramUsage} />
          <CardInfoField
            label="Network I/O"
            value={`↓ ${machine.network.down}  ↑ ${machine.network.up}`}
          />
        </div>
        <div className="relative">
          <div className="absolute inset-0 flex flex-col">
            <h3 className="m-auto">Jobs</h3>

            <div className="overflow-y-auto flex-1">
              <table className="table-fixed w-full">
                <thead>
                  <tr className="border-b-gray-400 border-b">
                    <th className="text-sm text-left py-1">ID</th>
                    <th className="text-sm text-left py-1">Accessed</th>
                  </tr>
                </thead>
                <tbody>
                  {machine.jobs.map((job) => (
                    <tr key={job.id} className="border-b-gray-400 border-b">
                      <td className="text-sm py-1">
                        <Link
                          to="/jobs"
                          hash={job.id}
                          className="text-accent underline"
                        >
                          {job.id}
                        </Link>
                      </td>
                      <td className="text-sm py-1">
                        {format(job.accessed, 'yyyy-MM-dd')}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        </div>
      </div>

      {/* Component Usage Chart */}
      <Chart data={machine.usageHistory} />
    </Card>
  )
}

function RouteComponent() {
  function handleProvision(machineId: string) {
    // TODO: Implement
    console.log(`Provisioned ${machineId}`)
  }

  const machines = Route.useLoaderData()

  return (
    <div className="px-16 py-8">
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-10">
        {machines.map((machine) => (
          <MachineCard
            key={machine.id}
            machine={machine}
            onProvision={handleProvision}
          />
        ))}
      </div>
    </div>
  )
}

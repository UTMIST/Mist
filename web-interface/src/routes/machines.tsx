import { createFileRoute, Link } from '@tanstack/react-router'
import type { Machine } from '#/types/machine.ts'
import Card, { CardHeader, CardInfoField } from '#/components/Card.tsx'
import { Button } from '#/components/Buttons.tsx'
import Chart from '#/components/Chart.tsx'
import { generateSampleUsageData } from '#/routes/jobs.tsx'
import { format } from 'date-fns'

export const Route = createFileRoute('/machines')({
  component: RouteComponent,
  loader: loadMachines,
})

function loadMachines(): Machine[] {
  // TODO: Implement actual API calling. For now we just return sample data

  const machines: Machine[] = [
    {
      id: 'tenstorrent_1',
      gpu: 'TT-Blackhole',
      cpu: 'TT-Ascalon',
      jobs: [
        {
          id: 'f3xkcd',
          created: new Date('2026-03-01T22:32:00'),
          accessed: new Date('2026-03-05T11:01:00'),
          dockerImage: 'utmist/mpt-3.5-turbo',
          usageHistory: [
            generateSampleUsageData(),
            generateSampleUsageData(),
            generateSampleUsageData(),
            generateSampleUsageData(),
            generateSampleUsageData(),
          ],
        },
        {
          id: 'f3xkcd',
          created: new Date('2026-03-01T22:32:00'),
          accessed: new Date('2026-03-05T11:01:00'),
          dockerImage: 'utmist/mpt-3.5-turbo',
          usageHistory: [
            generateSampleUsageData(),
            generateSampleUsageData(),
            generateSampleUsageData(),
            generateSampleUsageData(),
            generateSampleUsageData(),
          ],
        },
        {
          id: 'f3xkcd',
          created: new Date('2026-03-01T22:32:00'),
          accessed: new Date('2026-03-05T11:01:00'),
          dockerImage: 'utmist/mpt-3.5-turbo',
          usageHistory: [
            generateSampleUsageData(),
            generateSampleUsageData(),
            generateSampleUsageData(),
            generateSampleUsageData(),
            generateSampleUsageData(),
          ],
        },
        {
          id: 'f3xkcd',
          created: new Date('2026-03-01T22:32:00'),
          accessed: new Date('2026-03-05T11:01:00'),
          dockerImage: 'utmist/mpt-3.5-turbo',
          usageHistory: [
            generateSampleUsageData(),
            generateSampleUsageData(),
            generateSampleUsageData(),
            generateSampleUsageData(),
            generateSampleUsageData(),
          ],
        },
        {
          id: 'f3xkcd',
          created: new Date('2026-03-01T22:32:00'),
          accessed: new Date('2026-03-05T11:01:00'),
          dockerImage: 'utmist/mpt-3.5-turbo',
          usageHistory: [
            generateSampleUsageData(),
            generateSampleUsageData(),
            generateSampleUsageData(),
            generateSampleUsageData(),
            generateSampleUsageData(),
          ],
        },
        {
          id: 'f3xkcd',
          created: new Date('2026-03-01T22:32:00'),
          accessed: new Date('2026-03-05T11:01:00'),
          dockerImage: 'utmist/mpt-3.5-turbo',
          usageHistory: [
            generateSampleUsageData(),
            generateSampleUsageData(),
            generateSampleUsageData(),
            generateSampleUsageData(),
            generateSampleUsageData(),
          ],
        },
        {
          id: 'f3xkcd',
          created: new Date('2026-03-01T22:32:00'),
          accessed: new Date('2026-03-05T11:01:00'),
          dockerImage: 'utmist/mpt-3.5-turbo',
          usageHistory: [
            generateSampleUsageData(),
            generateSampleUsageData(),
            generateSampleUsageData(),
            generateSampleUsageData(),
            generateSampleUsageData(),
          ],
        },
        {
          id: 'f3xkcd',
          created: new Date('2026-03-01T22:32:00'),
          accessed: new Date('2026-03-05T11:01:00'),
          dockerImage: 'utmist/mpt-3.5-turbo',
          usageHistory: [
            generateSampleUsageData(),
            generateSampleUsageData(),
            generateSampleUsageData(),
            generateSampleUsageData(),
            generateSampleUsageData(),
          ],
        },
      ],
      diskUsage: '70GB/128GB (55%)',
      cpuUsage: '95%',
      ramUsage: '16.7GB/32GB (52%)',
      network: {
        down: '1.2 GB/s',
        up: '340 MB/s',
      },
      ip: '11.22.33.44',
      usageHistory: [
        generateSampleUsageData(),
        generateSampleUsageData(),
        generateSampleUsageData(),
        generateSampleUsageData(),
        generateSampleUsageData(),
      ],
    },
    {
      id: 'tenstorrent_2',
      gpu: 'TT-Blackhole',
      cpu: 'TT-Ascalon',
      jobs: [],
      diskUsage: '23GB/128GB (55%)',
      cpuUsage: '5%',
      ramUsage: '6.7GB/32GB (52%)',
      network: {
        down: '5 GB/s',
        up: '32 MB/s',
      },
      ip: '11.22.33.45',
      usageHistory: [
        generateSampleUsageData(),
        generateSampleUsageData(),
        generateSampleUsageData(),
        generateSampleUsageData(),
        generateSampleUsageData(),
      ],
    },
    {
      id: 'tenstorrent_3',
      gpu: 'TT-Blackhole',
      cpu: 'TT-Ascalon',
      jobs: [
        {
          id: 'm4n5p6',
          created: new Date('2026-03-01T22:32:00'),
          accessed: new Date('2026-03-05T11:01:00'),
          dockerImage: 'utmist/mpt-3.5-turbo',
          usageHistory: [
            generateSampleUsageData(),
            generateSampleUsageData(),
            generateSampleUsageData(),
            generateSampleUsageData(),
            generateSampleUsageData(),
          ],
        },
        {
          id: 'x9y8z7',
          created: new Date('2026-03-01T22:32:00'),
          accessed: new Date('2026-03-05T11:01:00'),
          dockerImage: 'utmist/mpt-3.5-turbo',
          usageHistory: [
            generateSampleUsageData(),
            generateSampleUsageData(),
            generateSampleUsageData(),
            generateSampleUsageData(),
            generateSampleUsageData(),
          ],
        },
      ],
      diskUsage: '70GB/128GB (55%)',
      cpuUsage: '95%',
      ramUsage: '16.7GB/32GB (52%)',
      network: {
        down: '1.2 GB/s',
        up: '340 MB/s',
      },
      ip: '11.22.33.44',
      usageHistory: [
        generateSampleUsageData(),
        generateSampleUsageData(),
        generateSampleUsageData(),
        generateSampleUsageData(),
        generateSampleUsageData(),
      ],
    },
    {
      id: 'tenstorrent_4',
      gpu: 'TT-Blackhole',
      cpu: 'TT-Ascalon',
      jobs: [
        {
          id: 'f3xkcd',
          created: new Date('2026-03-01T22:32:00'),
          accessed: new Date('2026-03-05T11:01:00'),
          dockerImage: 'utmist/mpt-3.5-turbo',
          usageHistory: [
            generateSampleUsageData(),
            generateSampleUsageData(),
            generateSampleUsageData(),
            generateSampleUsageData(),
            generateSampleUsageData(),
          ],
        },
        {
          id: 'f3xkcd',
          created: new Date('2026-03-01T22:32:00'),
          accessed: new Date('2026-03-05T11:01:00'),
          dockerImage: 'utmist/mpt-3.5-turbo',
          usageHistory: [
            generateSampleUsageData(),
            generateSampleUsageData(),
            generateSampleUsageData(),
            generateSampleUsageData(),
            generateSampleUsageData(),
          ],
        },
      ],
      diskUsage: '70GB/128GB (55%)',
      cpuUsage: '95%',
      ramUsage: '16.7GB/32GB (52%)',
      network: {
        down: '1.2 GB/s',
        up: '340 MB/s',
      },
      ip: '11.22.33.44',
      usageHistory: [
        generateSampleUsageData(),
        generateSampleUsageData(),
        generateSampleUsageData(),
        generateSampleUsageData(),
        generateSampleUsageData(),
      ],
    },
    {
      id: 'tenstorrent_5',
      gpu: 'TT-Blackhole',
      cpu: 'TT-Ascalon',
      jobs: [
        {
          id: 'f3xkcd',
          created: new Date('2026-03-01T22:32:00'),
          accessed: new Date('2026-03-05T11:01:00'),
          dockerImage: 'utmist/mpt-3.5-turbo',
          usageHistory: [
            generateSampleUsageData(),
            generateSampleUsageData(),
            generateSampleUsageData(),
            generateSampleUsageData(),
            generateSampleUsageData(),
          ],
        },
        {
          id: 'f3xkcd',
          created: new Date('2026-03-01T22:32:00'),
          accessed: new Date('2026-03-05T11:01:00'),
          dockerImage: 'utmist/mpt-3.5-turbo',
          usageHistory: [
            generateSampleUsageData(),
            generateSampleUsageData(),
            generateSampleUsageData(),
            generateSampleUsageData(),
            generateSampleUsageData(),
          ],
        },
      ],
      diskUsage: '70GB/128GB (55%)',
      cpuUsage: '95%',
      ramUsage: '16.7GB/32GB (52%)',
      network: {
        down: '1.2 GB/s',
        up: '340 MB/s',
      },
      ip: '11.22.33.44',
      usageHistory: [
        generateSampleUsageData(),
        generateSampleUsageData(),
        generateSampleUsageData(),
        generateSampleUsageData(),
        generateSampleUsageData(),
      ],
    },
    {
      id: 'tenstorrent_6',
      gpu: 'TT-Blackhole',
      cpu: 'TT-Ascalon',
      jobs: [
        {
          id: 'f3xkcd',
          created: new Date('2026-03-01T22:32:00'),
          accessed: new Date('2026-03-05T11:01:00'),
          dockerImage: 'utmist/mpt-3.5-turbo',
          usageHistory: [
            generateSampleUsageData(),
            generateSampleUsageData(),
            generateSampleUsageData(),
            generateSampleUsageData(),
            generateSampleUsageData(),
          ],
        },
        {
          id: 'f3xkcd',
          created: new Date('2026-03-01T22:32:00'),
          accessed: new Date('2026-03-05T11:01:00'),
          dockerImage: 'utmist/mpt-3.5-turbo',
          usageHistory: [
            generateSampleUsageData(),
            generateSampleUsageData(),
            generateSampleUsageData(),
            generateSampleUsageData(),
            generateSampleUsageData(),
          ],
        },
      ],
      diskUsage: '70GB/128GB (55%)',
      cpuUsage: '95%',
      ramUsage: '16.7GB/32GB (52%)',
      network: {
        down: '1.2 GB/s',
        up: '340 MB/s',
      },
      ip: '11.22.33.44',
      usageHistory: [
        generateSampleUsageData(),
        generateSampleUsageData(),
        generateSampleUsageData(),
        generateSampleUsageData(),
        generateSampleUsageData(),
      ],
    },
  ]

  return machines
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

import { createFileRoute } from '@tanstack/react-router'
import Card, { CardHeader, CardInfoField } from '#/components/Card.tsx'
import { useState } from 'react'
import { Button } from '#/components/Buttons.tsx'
import Chart from '#/components/Chart.tsx'
import type {Job, UsageData} from "#/types/job.ts";

export const Route = createFileRoute('/jobs')({
  component: JobsPage,
  loader: loadJobs
})

function loadJobs(): Job[] {
  const jobs: Job[] = [
    {
      id: 'job-1',
      name: 'f3xkcd',
      created: '2026-03-01 10:32 PM',
      accessed: '2026-03-05 11:01 AM',
      machine: {
        id: "tenstorrent_1",
        gpu: "TT-Blackhole",
        cpu: "TT-Ascalon",
        jobs: [],
        diskUsage: "70GB/128GB (55%)",
        cpuUsage: "95%",
        ramUsage: "16.7GB/32GB (52%)",
        network: {
          down: "1.2 GB/s",
          up: "340 MB/s"
        },
        ip: "11.22.33.44"
      },
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
      id: 'job-2',
      name: 'f3xkcd',
      created: '2026-03-01 10:32 PM',
      accessed: '2026-03-05 11:01 AM',
      machine: {
        id: "tenstorrent_1",
        gpu: "TT-Blackhole",
        cpu: "TT-Ascalon",
        jobs: [],
        diskUsage: "70GB/128GB (55%)",
        cpuUsage: "95%",
        ramUsage: "16.7GB/32GB (52%)",
        network: {
          down: "1.2 GB/s",
          up: "340 MB/s"
        },
        ip: "11.22.33.44"
      },
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
      id: 'job-3',
      name: 'f3xkcd',
      created: '2026-03-01 10:32 PM',
      accessed: '2026-03-05 11:01 AM',
      machine: {
        id: "tenstorrent_1",
        gpu: "TT-Blackhole",
        cpu: "TT-Ascalon",
        jobs: [],
        diskUsage: "70GB/128GB (55%)",
        cpuUsage: "95%",
        ramUsage: "16.7GB/32GB (52%)",
        network: {
          down: "1.2 GB/s",
          up: "340 MB/s"
        },
        ip: "11.22.33.44"
      },
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
      id: 'job-4',
      name: 'f3xkcd',
      created: '2026-03-01 10:32 PM',
      accessed: '2026-03-05 11:01 AM',
      machine: {
        id: "tenstorrent_1",
        gpu: "TT-Blackhole",
        cpu: "TT-Ascalon",
        jobs: [],
        diskUsage: "70GB/128GB (55%)",
        cpuUsage: "95%",
        ramUsage: "16.7GB/32GB (52%)",
        network: {
          down: "1.2 GB/s",
          up: "340 MB/s"
        },
        ip: "11.22.33.44"
      },
      dockerImage: 'utmist/mpt-3.5-turbo',
      usageHistory: [
        generateSampleUsageData(),
        generateSampleUsageData(),
        generateSampleUsageData(),
        generateSampleUsageData(),
        generateSampleUsageData(),
      ],
    },
  ]

  return jobs
}

function generateSampleUsageData(): UsageData {
  const components = ['GPU', 'CPU', 'RAM']

  return {
    component: components[Math.floor(Math.random() * components.length)], // of course these should be ordered, but this is sample data. When we use the real Grafana data we will throw this out anyway.
    observations: Array.from({ length: 25 }, (_, i) => {
      if (i < 6) return 5 + Math.random() * 10
      if (i < 10) return 10 + Math.random() * 20
      if (i < 14) return 50 + Math.random() * 45
      if (i < 18) return 70 + Math.random() * 25
      return 30 + Math.random() * 30
    }),
  }
}

type JobCardProps = {
  job: Job
  onStart: (id: string) => void
  onShutdown: (id: string) => void
  onRestart: (id: string) => void
  onDelete: (id: string) => void
}

function JobCard({
  job,
  onStart,
  onShutdown,
  onRestart,
  onDelete,
}: JobCardProps) {
  const [componentIndex, setComponentIndex] = useState(0)
  const totalComponents = job.usageHistory.length

  const prevComponent = () =>
    setComponentIndex((i) => (i - 1 + totalComponents) % totalComponents)
  const nextComponent = () =>
    setComponentIndex((i) => (i + 1) % totalComponents)

  return (
    <Card>
      <CardHeader header={job.name}>
        <Button onClick={() => onStart(job.id)} variant="success" fontSize="xs">
          Start
        </Button>
        <Button
          onClick={() => onShutdown(job.id)}
          variant="warning"
          fontSize="xs"
        >
          Shutdown
        </Button>
        <Button
          onClick={() => onRestart(job.id)}
          variant="warning"
          fontSize="xs"
        >
          Restart
        </Button>
        <Button onClick={() => onDelete(job.id)} variant="danger" fontSize="xs">
          Delete
        </Button>
      </CardHeader>

      {/* Info grid */}
      <div className="grid grid-cols-3 gap-x-6 gap-y-3">
        <CardInfoField label="Created" value={job.created} />
        <CardInfoField label="Machine" value={job.machine.id} link="#" />
        <CardInfoField label="Disk Usage" value={job.machine.diskUsage} />
        <CardInfoField label="Accessed" value={job.accessed} />
        <CardInfoField label="GPU" value={job.machine.gpu} />
        <CardInfoField label="CPU Utilization" value={job.machine.cpuUsage} />
        <CardInfoField label="Docker Image" value={job.dockerImage} link="#" />
        <CardInfoField label="CPU" value={job.machine.cpu} />
        <CardInfoField
          label="Network I/O"
          value={`↓ ${job.machine.network.down}  ↑ ${job.machine.network.up}`}
        />
        <CardInfoField label="IP" value={job.machine.ip} />
        <CardInfoField label="RAM" value={job.machine.ramUsage} />
      </div>

      {/* Component Usage Chart */}
      <Chart
        data={job.usageHistory[componentIndex]}
        index={componentIndex}
        numComponents={totalComponents}
        onPrev={prevComponent}
        onNext={nextComponent}
      />
    </Card>
  )
}

function handleStart(id: string) {
  console.log('Start job:', id)
  // TODO: call API POST /jobs/{id}/start
}

function handleShutdown(id: string) {
  console.log('Shutdown job:', id)
  // TODO: call API POST /jobs/{id}/shutdown
}

function handleRestart(id: string) {
  console.log('Restart job:', id)
  // TODO: call API POST /jobs/{id}/restart
}

function handleDelete(id: string) {
  console.log('Delete job:', id)
  // TODO: call API DELETE /jobs/{id}
}

function JobsPage() {
  const jobs = Route.useLoaderData();

  return (
    <div className="px-16 py-8">
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-10">
        {jobs.map((job) => (
          <JobCard
            key={job.id}
            job={job}
            onStart={handleStart}
            onShutdown={handleShutdown}
            onRestart={handleRestart}
            onDelete={handleDelete}
          />
        ))}
      </div>
    </div>
  )
}

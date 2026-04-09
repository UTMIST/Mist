import { createFileRoute } from '@tanstack/react-router'
import Card, { CardHeader, CardInfoField } from '#/components/Card.tsx'
import { useState } from 'react'
import { Button } from '#/components/Buttons.tsx'
import Chart from '#/components/Chart.tsx'

export const Route = createFileRoute('/jobs')({
  component: JobsPage,
})

export type UsageData = {
  component: string // Component that we observe. E.g. GPU, CPU, etc.
  observations: number[]
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

export type Job = {
  id: string
  name: string
  created: string
  accessed: string
  machine: string
  gpu: string
  cpu: string
  dockerImage: string
  ip: string
  ram: string
  diskUsage: string
  cpuUtilization: string
  networkIO: { down: string; up: string }
  usageHistory: UsageData[]
}

const sampleJobs: Job[] = [
  {
    id: 'job-1',
    name: 'f3xkcd',
    created: '2026-03-01 10:32 PM',
    accessed: '2026-03-05 11:01 AM',
    machine: 'Tenstorrent_1',
    gpu: 'TT-Blackhole',
    cpu: 'TT-Ascalon',
    dockerImage: 'utmist/mpt-3.5-turbo',
    ip: '101.101.123.456',
    ram: '32GB',
    diskUsage: '70GB/128GB (55%)',
    cpuUtilization: '95%',
    networkIO: { down: '1.2 GB/s', up: '340 MB/s' },
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
    machine: 'Tenstorrent_1',
    gpu: 'TT-Blackhole',
    cpu: 'TT-Ascalon',
    dockerImage: 'utmist/mpt-3.5-turbo',
    ip: '101.101.123.456',
    ram: '32GB',
    diskUsage: '70GB/128GB (55%)',
    cpuUtilization: '95%',
    networkIO: { down: '1.2 GB/s', up: '340 MB/s' },
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
    machine: 'Tenstorrent_1',
    gpu: 'TT-Blackhole',
    cpu: 'TT-Ascalon',
    dockerImage: 'utmist/mpt-3.5-turbo',
    ip: '101.101.123.456',
    ram: '32GB',
    diskUsage: '70GB/128GB (55%)',
    cpuUtilization: '95%',
    networkIO: { down: '1.2 GB/s', up: '340 MB/s' },
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
    machine: 'Tenstorrent_1',
    gpu: 'TT-Blackhole',
    cpu: 'TT-Ascalon',
    dockerImage: 'utmist/mpt-3.5-turbo',
    ip: '101.101.123.456',
    ram: '32GB',
    diskUsage: '70GB/128GB (55%)',
    cpuUtilization: '95%',
    networkIO: { down: '1.2 GB/s', up: '340 MB/s' },
    usageHistory: [
      generateSampleUsageData(),
      generateSampleUsageData(),
      generateSampleUsageData(),
      generateSampleUsageData(),
      generateSampleUsageData(),
    ],
  },
]

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
        <CardInfoField label="Machine" value={job.machine} link="#" />
        <CardInfoField label="Disk Usage" value={job.diskUsage} />
        <CardInfoField label="Accessed" value={job.accessed} />
        <CardInfoField label="GPU" value={job.gpu} />
        <CardInfoField label="CPU Utilization" value={job.cpuUtilization} />
        <CardInfoField label="Docker Image" value={job.dockerImage} link="#" />
        <CardInfoField label="CPU" value={job.cpu} />
        <CardInfoField
          label="Network I/O"
          value={`↓ ${job.networkIO.down}  ↑ ${job.networkIO.up}`}
        />
        <CardInfoField label="IP" value={job.ip} />
        <CardInfoField label="RAM" value={job.ram} />
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
  return (
    <div className="px-16 py-8">
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-10">
        {sampleJobs.map((job) => (
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

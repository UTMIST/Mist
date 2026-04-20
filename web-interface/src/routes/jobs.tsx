import { createFileRoute } from '@tanstack/react-router'
import Card, { CardHeader, CardInfoField } from '#/components/Card.tsx'
import { Button } from '#/components/Buttons.tsx'
import Chart from '#/components/Chart.tsx'
import type { Job } from '#/types/job.ts'
import { format } from 'date-fns'
import {generateSampleData} from "#/util.ts";

export const Route = createFileRoute('/jobs')({
  component: JobsPage,
  loader: loadJobs,
})

function loadJobs(): Job[] {
  const data = generateSampleData()

  return data.jobs
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
  return (
    <Card id={job.id}>
      <CardHeader header={job.id}>
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
        <CardInfoField
          label="Created"
          value={format(job.created, 'yyyy-MM-dd hh:mm a')}
        />
        <CardInfoField
          label="Machine"
          value={job.machine.id}
          link="/machines"
          hash={job.machine.id}
        />
        <CardInfoField label="Disk Usage" value={job.machine.diskUsage} />
        <CardInfoField
          label="Accessed"
          value={format(job.accessed, 'yyyy-MM-dd hh:mm a')}
        />
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
      <Chart data={job.usageHistory} />
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
  const jobs = Route.useLoaderData()

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

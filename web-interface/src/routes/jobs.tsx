import { createFileRoute } from '@tanstack/react-router'
import JobCard from '#/components/JobCard.tsx'
import type { Job } from '#/components/JobCard.tsx'

export const Route = createFileRoute('/jobs')({
  component: JobsPage,
})

// Sample GPU utilization data (24 data points for 24 hours)
function generateGpuData(): number[] {
  return Array.from({ length: 25 }, (_, i) => {
    if (i < 6) return 5 + Math.random() * 10
    if (i < 10) return 10 + Math.random() * 20
    if (i < 14) return 50 + Math.random() * 45
    if (i < 18) return 70 + Math.random() * 25
    return 30 + Math.random() * 30
  })
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
    gpuHistory: [
      generateGpuData(),
      generateGpuData(),
      generateGpuData(),
      generateGpuData(),
      generateGpuData(),
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
    gpuHistory: [
      generateGpuData(),
      generateGpuData(),
      generateGpuData(),
      generateGpuData(),
      generateGpuData(),
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
    gpuHistory: [
      generateGpuData(),
      generateGpuData(),
      generateGpuData(),
      generateGpuData(),
      generateGpuData(),
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
    gpuHistory: [
      generateGpuData(),
      generateGpuData(),
      generateGpuData(),
      generateGpuData(),
      generateGpuData(),
    ],
  },
]

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

import {createFileRoute} from '@tanstack/react-router'
import Card, {CardHeader, CardInfoField} from '#/components/Card.tsx'
import {ChevronLeft, ChevronRight} from "lucide-react";
import {useState} from "react";
import {ButtonDanger, ButtonSuccess, ButtonWarning} from "#/components/Buttons.tsx";

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
  gpuHistory: number[][] // array of [hour, percentage] data points per GPU
}

type JobCardProps = {
  job: Job
  onStart: (id: string) => void
  onShutdown: (id: string) => void
  onRestart: (id: string) => void
  onDelete: (id: string) => void
}

function GpuChart({
                    data,
                    gpuIndex,
                    totalGpus,
                    onPrev,
                    onNext,
                  }: {
  data: number[]
  gpuIndex: number
  totalGpus: number
  onPrev: () => void
  onNext: () => void
}) {
  const max = 100
  const width = 600
  const height = 180
  const padLeft = 40
  const padRight = 10
  const padTop = 10
  const padBottom = 30
  const chartW = width - padLeft - padRight
  const chartH = height - padTop - padBottom

  const points = data.map((val, i) => {
    const x = padLeft + (i / (data.length - 1)) * chartW
    const y = padTop + chartH - (val / max) * chartH
    return `${x},${y}`
  })

  const areaPoints = [
    `${padLeft},${padTop + chartH}`,
    ...points,
    `${padLeft + chartW},${padTop + chartH}`,
  ].join(' ')

  const linePoints = points.join(' ')

  const yLabels = [100, 80, 60, 40, 20, 0]
  const xLabels = [
    '00:00',
    '02:00',
    '04:00',
    '06:00',
    '08:00',
    '10:00',
    '12:00',
    '14:00',
    '16:00',
    '18:00',
    '20:00',
    '22:00',
    '24:00',
  ]

  return (
    <div className="mt-4">
      <h3 className="text-center font-semibold text-sm mb-1">GPU</h3>
      <div className="flex items-center gap-2">
        <button
          onClick={onPrev}
          className="text-gray-400 hover:text-main p-1"
          aria-label="Previous GPU"
        >
          <ChevronLeft size={24} />
        </button>
        <svg viewBox={`0 0 ${width} ${height}`} className="flex-1">
          {/* Y-axis labels and grid */}
          {yLabels.map((label) => {
            const y = padTop + chartH - (label / max) * chartH
            return (
              <g key={label}>
                <text
                  x={padLeft - 5}
                  y={y + 3}
                  textAnchor="end"
                  fontSize="9"
                  fill="#999"
                >
                  {label}%
                </text>
                <line
                  x1={padLeft}
                  y1={y}
                  x2={padLeft + chartW}
                  y2={y}
                  stroke="#e5e7eb"
                  strokeWidth="0.5"
                />
              </g>
            )
          })}
          {/* X-axis labels */}
          {xLabels.map((label, i) => {
            const x = padLeft + (i / (xLabels.length - 1)) * chartW
            return (
              <text
                key={label}
                x={x}
                y={height - 5}
                textAnchor="middle"
                fontSize="8"
                fill="#999"
              >
                {label}
              </text>
            )
          })}
          {/* Area fill */}
          <polygon points={areaPoints} fill="rgba(234,179,8,0.15)" />
          {/* Line */}
          <polyline
            points={linePoints}
            fill="none"
            stroke="#eab308"
            strokeWidth="2"
          />
        </svg>
        <button
          onClick={onNext}
          className="text-gray-400 hover:text-main p-1"
          aria-label="Next GPU"
        >
          <ChevronRight size={24} />
        </button>
      </div>
      {/* Pagination dots */}
      <div className="flex justify-center gap-1.5 mt-1">
        {Array.from({ length: totalGpus }).map((_, i) => (
          <span
            key={i}
            className={`w-2 h-2 rounded-full ${i === gpuIndex ? 'bg-main' : 'bg-gray-300'}`}
          />
        ))}
      </div>
    </div>
  )
}

function JobCard({
                   job,
                   onStart,
                   onShutdown,
                   onRestart,
                   onDelete,
                 }: JobCardProps) {
  const [gpuIndex, setGpuIndex] = useState(0)
  const totalGpus = job.gpuHistory.length

  const prevGpu = () => setGpuIndex((i) => (i - 1 + totalGpus) % totalGpus)
  const nextGpu = () => setGpuIndex((i) => (i + 1) % totalGpus)

  return (
    <Card>
      <CardHeader header={job.name}>
        <ButtonSuccess onClick={() => onStart(job.id)} text="Start" />
        <ButtonWarning onClick={() => onShutdown(job.id)} text="Shutdown" />
        <ButtonWarning onClick={() => onRestart(job.id)} text="Restart" />
        <ButtonDanger onClick={() => onDelete(job.id)} text="Delete" />
      </CardHeader>

      {/* Info grid */}
      <div className="grid grid-cols-3 gap-x-6 gap-y-3">
        <CardInfoField label="Created" value={job.created} />
        <CardInfoField label="Machine" value={job.machine} link="#" />
        <CardInfoField label="Disk Usage" value={job.diskUsage} />
        <CardInfoField label="Accessed" value={job.accessed} />
        <CardInfoField label="GPU" value={job.gpu} />
        <CardInfoField label="CPU Utilization" value={job.cpuUtilization} />
        <CardInfoField
          label="Docker Image"
          value={job.dockerImage}
          link="#"
        />
        <CardInfoField label="CPU" value={job.cpu} />
        <CardInfoField
          label="Network I/O"
          value={`↓ ${job.networkIO.down}  ↑ ${job.networkIO.up}`}
        />
        <CardInfoField label="IP" value={job.ip} />
        <CardInfoField label="RAM" value={job.ram} />
      </div>

      {/* GPU Chart */}
      <GpuChart
        data={job.gpuHistory[gpuIndex]}
        gpuIndex={gpuIndex}
        totalGpus={totalGpus}
        onPrev={prevGpu}
        onNext={nextGpu}
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

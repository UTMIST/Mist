import { useState } from 'react'
import { Link } from '@tanstack/react-router'
import {
  ChevronLeft,
  ChevronRight,
} from 'lucide-react'

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

function InfoField({
  label,
  value,
  link,
}: {
  label: string
  value: string
  link?: string
}) {
  return (
    <div>
      <div className="text-xs text-gray-500 font-medium">{label}</div>
      {link ? (
        <Link to={link} className="text-sm text-accent underline">
          {value}
        </Link>
      ) : (
        <div className="text-sm font-medium">{value}</div>
      )}
    </div>
  )
}

export default function JobCard({
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
    <div className="border border-gray-200 rounded-xl p-5">
      {/* Header */}
      <div className="flex items-center justify-between mb-4">
        <h2 className="text-lg font-bold">{job.name}</h2>
        <div className="flex gap-2">
          <button
            onClick={() => onStart(job.id)}
            className="px-3 py-1 text-xs font-semibold rounded bg-green-500 text-white hover:bg-green-600"
          >
            Start
          </button>
          <button
            onClick={() => onShutdown(job.id)}
            className="px-3 py-1 text-xs font-semibold rounded bg-yellow-400 text-white hover:bg-yellow-500"
          >
            Shutdown
          </button>
          <button
            onClick={() => onRestart(job.id)}
            className="px-3 py-1 text-xs font-semibold rounded bg-yellow-400 text-white hover:bg-yellow-500"
          >
            Restart
          </button>
          <button
            onClick={() => onDelete(job.id)}
            className="px-3 py-1 text-xs font-semibold rounded bg-red-500 text-white hover:bg-red-600"
          >
            Delete
          </button>
        </div>
      </div>

      {/* Info grid */}
      <div className="grid grid-cols-3 gap-x-6 gap-y-3">
        <InfoField label="Created" value={job.created} />
        <InfoField label="Machine" value={job.machine} link="#" />
        <InfoField label="Disk Usage" value={job.diskUsage} />
        <InfoField label="Accessed" value={job.accessed} />
        <InfoField label="GPU" value={job.gpu} />
        <InfoField label="CPU Utilization" value={job.cpuUtilization} />
        <InfoField
          label="Docker Image"
          value={job.dockerImage}
          link="#"
        />
        <InfoField label="CPU" value={job.cpu} />
        <InfoField
          label="Network I/O"
          value={`↓ ${job.networkIO.down}  ↑ ${job.networkIO.up}`}
        />
        <InfoField label="IP" value={job.ip} />
        <InfoField label="RAM" value={job.ram} />
      </div>

      {/* GPU Chart */}
      <GpuChart
        data={job.gpuHistory[gpuIndex]}
        gpuIndex={gpuIndex}
        totalGpus={totalGpus}
        onPrev={prevGpu}
        onNext={nextGpu}
      />
    </div>
  )
}

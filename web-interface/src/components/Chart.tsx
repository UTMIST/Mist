import { ChevronLeft, ChevronRight } from 'lucide-react'
import type { UsageData } from '#/routes/jobs.tsx'

export default function Chart({
  data,
  index,
  numComponents,
  onPrev,
  onNext,
}: {
  data: UsageData
  index: number
  numComponents: number
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

  const points = data.observations.map((val, i) => {
    const x = padLeft + (i / (data.observations.length - 1)) * chartW
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
      <h3 className="text-center font-semibold text-sm mb-1">
        {data.component}
      </h3>
      <div className="flex items-center gap-2">
        <button
          onClick={onPrev}
          className="text-gray-400 hover:text-main p-1"
          aria-label="Previous Component"
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
          aria-label="Next Component"
        >
          <ChevronRight size={24} />
        </button>
      </div>
      {/* Pagination dots */}
      <div className="flex justify-center gap-1.5 mt-1">
        {Array.from({ length: numComponents }).map((_, i) => (
          <span
            key={i}
            className={`w-2 h-2 rounded-full ${i === index ? 'bg-main' : 'bg-gray-300'}`}
          />
        ))}
      </div>
    </div>
  )
}

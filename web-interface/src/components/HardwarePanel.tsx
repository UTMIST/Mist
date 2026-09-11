import { Cpu } from 'lucide-react'
import type { Hardware } from '#/localCompute'

export function HardwarePanel({
  hardware,
  error,
}: {
  hardware: Hardware | null
  error: string
}) {
  const problem = error || hardware?.error
  return (
    <section
      className="rounded-2xl border border-slate-200 bg-white p-6"
      aria-label="Local GPU"
    >
      <div className="flex flex-wrap items-center justify-between gap-4">
        <div className="flex items-center gap-4">
          <span className="rounded-xl bg-slate-100 p-3">
            <Cpu size={25} />
          </span>
          <div>
            <p className="text-xs font-semibold uppercase tracking-wider text-slate-500">
              Your machine
            </p>
            <h2 className="mt-1 text-lg font-bold">
              {error
                ? 'Worker unreachable'
                : (hardware?.name ??
                  (problem ? 'GPU unavailable' : 'Detecting NVIDIA GPU…'))}
            </h2>
          </div>
        </div>
        <span
          className={
            'rounded-full px-3 py-1 text-xs font-semibold ' +
            (hardware?.available && !problem
              ? 'bg-emerald-50 text-emerald-700'
              : 'bg-slate-100 text-slate-500')
          }
        >
          {hardware?.available && !problem ? 'Connected' : 'Not ready'}
        </span>
      </div>
      {problem && (
        <p role="alert" className="mt-4 break-words text-sm text-red-700">
          {problem}
        </p>
      )}
      {hardware?.available && !problem && (
        <div className="mt-5 flex flex-wrap gap-x-8 gap-y-2 text-sm text-slate-600">
          <span>
            VRAM:{' '}
            {hardware.memory_total_mib !== undefined
              ? (hardware.memory_total_mib / 1024).toFixed(1) + ' GB'
              : 'Unavailable'}
          </span>
          <span>
            In use:{' '}
            {hardware.memory_used_mib !== undefined
              ? (hardware.memory_used_mib / 1024).toFixed(1) + ' GB'
              : 'Unavailable'}
          </span>
          <span>
            GPU utilization:{' '}
            {hardware.utilization_percent !== undefined
              ? hardware.utilization_percent + '%'
              : 'Unavailable'}
          </span>
          <span>Driver: {hardware.driver_version}</span>
          <span className="text-xs text-slate-400">
            Sampled every 15 seconds
          </span>
        </div>
      )}
    </section>
  )
}

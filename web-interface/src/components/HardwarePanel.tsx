import Card from '#/components/Card.tsx'
import type { HardwareSnapshot } from '#/api.ts'

export const computeNames = {
  cpu: 'CPU',
  nvidia: 'NVIDIA',
  tenstorrent: 'Tenstorrent',
}

function memoryLabel(quantity: string) {
  const match = quantity.match(/^(\d+(?:\.\d+)?)(Ki|Mi|Gi|Ti)?$/)
  if (!match) return quantity
  const powers: Record<string, number> = { Ki: 1, Mi: 2, Gi: 3, Ti: 4 }
  const bytes = Number(match[1]) * 1024 ** (powers[match[2]] ?? 0)
  return `${(bytes / 1024 ** 3).toFixed(1)} GiB`
}

export function HardwarePanel({
  hardware,
  error,
  loading,
}: {
  hardware: HardwareSnapshot | null
  error: string
  loading: boolean
}) {
  return (
    <Card>
      <h2 className="text-lg font-semibold mb-3">Hardware availability</h2>
      {loading && <p>Loading hardware…</p>}
      {error && (
        <p role="alert" className="text-red-700">
          Hardware unavailable: {error}
        </p>
      )}
      {hardware && (
        <>
          <div className="overflow-x-auto">
            <table className="w-full text-sm text-left">
              <caption className="sr-only">
                Current accelerator allocations
              </caption>
              <thead>
                <tr>
                  {[
                    'Hardware',
                    'Machine',
                    'Total',
                    'Allocated',
                    'Available',
                    'Placement',
                  ].map((label) => (
                    <th key={label} scope="col" className="py-2 pr-4">
                      {label}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {hardware.pools.map((pool) => (
                  <tr
                    key={`${pool.node}-${pool.accelerator}`}
                    className="border-t border-gray-200"
                  >
                    <th scope="row" className="py-3 pr-4 font-medium">
                      {computeNames[pool.accelerator]} {pool.unit}s
                    </th>
                    <td className="pr-4">{pool.node}</td>
                    <td>{pool.total}</td>
                    <td>{pool.allocated}</td>
                    <td>
                      {error || pool.available === null
                        ? 'Unknown'
                        : pool.available}
                    </td>
                    <td>
                      {error ? 'Unknown' : pool.ready ? 'Ready' : 'Unavailable'}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <div className="mt-4 grid gap-2 md:grid-cols-2 text-sm">
            {hardware.machines.map((machine) => (
              <p key={machine.name}>
                <span className="font-medium">{machine.name}</span>:{' '}
                {machine.ready
                  ? machine.schedulable
                    ? 'Ready'
                    : 'Placement paused'
                  : 'Offline'}
                {machine.ready && (
                  <>
                    {' '}
                    · {machine.cpu_allocatable} CPU ·{' '}
                    {memoryLabel(machine.memory_allocatable)} RAM allocatable
                  </>
                )}
              </p>
            ))}
          </div>
          {hardware.pools
            .filter((pool) => pool.message)
            .map((pool) => (
              <p key={pool.accelerator} className="mt-2 text-sm">
                {computeNames[pool.accelerator]}: {pool.message}
              </p>
            ))}
          <p className="mt-3 text-xs text-gray-600">
            Updated {new Date(hardware.observed_at).toLocaleTimeString()}.
            Availability can change before submission. Jobs wait for resources.
            One Tenstorrent board contains two chips.
          </p>
        </>
      )}
    </Card>
  )
}

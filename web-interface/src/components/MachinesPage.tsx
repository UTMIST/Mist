import { jobsAPI } from '#/api.ts'
import { HardwarePanel } from '#/components/HardwarePanel.tsx'
import { usePolling } from '#/hooks/usePolling.ts'

export function MachinesPage() {
  const hardware = usePolling(jobsAPI.hardware)
  return (
    <div className="px-6 py-8 lg:px-16 space-y-6">
      <h1 className="text-2xl font-bold">Machines</h1>
      <HardwarePanel
        hardware={hardware.data}
        error={hardware.error}
        loading={hardware.loading}
      />
      <p className="text-sm text-gray-600">
        CPU and RAM figures are allocatable scheduling capacity, not live
        utilization. Jobs reserve whole NVIDIA GPUs or whole Tenstorrent boards.
      </p>
    </div>
  )
}

import { createFileRoute } from '@tanstack/react-router'
import { HardwarePanel } from '#/components/HardwarePanel'
import { useHardware } from '#/localCompute'

export const Route = createFileRoute('/machines')({ component: MachinePage })

function MachinePage() {
  const { hardware, error } = useHardware()
  return (
    <main className="mx-auto max-w-6xl space-y-6 px-5 py-10 sm:px-8">
      <h1 className="text-3xl font-bold">Local machine</h1>
      <p className="text-slate-600">
        Hardware reported by the NVIDIA driver on this computer.
      </p>
      <HardwarePanel hardware={hardware} error={error} />
    </main>
  )
}

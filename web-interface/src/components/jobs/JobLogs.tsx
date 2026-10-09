import { useCallback } from 'react'
import Card, { CardHeader } from '#/components/Card.tsx'
import { Button } from '#/components/Buttons.tsx'
import { jobsAPI } from '#/api.ts'
import { usePolling } from '#/hooks/usePolling.ts'

export function JobLogs({ id, onClose }: { id: string; onClose: () => void }) {
  const load = useCallback(
    (signal: AbortSignal) => jobsAPI.logs(id, signal),
    [id],
  )
  const result = usePolling(load, 3000)
  return (
    <Card>
      <CardHeader header={`Logs: ${id}`}>
        <Button onClick={onClose} variant="normal" fontSize="xs">
          Close
        </Button>
      </CardHeader>
      {result.error && (
        <p role="alert" className="text-red-700">
          {result.error}
        </p>
      )}
      <pre className="whitespace-pre-wrap break-all max-h-96 overflow-auto font-mono text-xs">
        {result.data?.logs ||
          (result.loading ? 'Loading logs…' : 'No output yet.')}
      </pre>
    </Card>
  )
}

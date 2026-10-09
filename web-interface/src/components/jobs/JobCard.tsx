import { useState } from 'react'
import { JobFiles } from '#/components/jobs/JobFiles.tsx'
import Card, { CardHeader, CardInfoField } from '#/components/Card.tsx'
import { Button } from '#/components/Buttons.tsx'
import type { Job } from '#/api.ts'

const statusNames = {
  Scheduled: 'Waiting',
  InProgress: 'Running',
  Success: 'Completed',
  Failure: 'Failed',
  Cancelled: 'Cancelled',
}

export function JobCard({
  job,
  cancelling,
  onLogs,
  onCancel,
}: {
  job: Job
  cancelling: boolean
  onLogs: () => void
  onCancel: () => void
}) {
  const [showFiles, setShowFiles] = useState(false)
  return (
    <Card>
      <CardHeader header={job.name}>
        <Button
          onClick={() => setShowFiles(!showFiles)}
          variant="normal"
          fontSize="xs"
        >
          Files
        </Button>
        <Button onClick={onLogs} variant="normal" fontSize="xs">
          Logs
        </Button>
        {(job.job_state === 'Scheduled' || job.job_state === 'InProgress') && (
          <Button
            onClick={onCancel}
            variant={cancelling ? 'disabled' : 'danger'}
            fontSize="xs"
          >
            {cancelling ? 'Cancelling…' : 'Cancel'}
          </Button>
        )}
      </CardHeader>
      <div className="grid grid-cols-2 gap-3 break-all">
        <CardInfoField label="Status" value={statusNames[job.job_state]} />
        <CardInfoField
          label="Compute"
          value={`${job.accelerator}${job.device_count ? ` × ${job.device_count}${job.accelerator === 'tenstorrent' ? ' boards' : ' GPUs'}` : ''}`}
        />
        <CardInfoField label="Job ID" value={job.id} />
        <CardInfoField
          label="Machine"
          value={job.node ?? 'Waiting for placement'}
        />
        <CardInfoField
          label="Created"
          value={new Date(job.created).toLocaleString()}
        />
        <CardInfoField
          label="Resources"
          value={`${job.cpu} CPU · ${job.memory} RAM`}
        />
        <CardInfoField label="Image" value={job.image} />
        <CardInfoField
          label="Exit code"
          value={job.exit_code === undefined ? '—' : String(job.exit_code)}
        />
        <CardInfoField
          label="Outputs"
          value={job.output_directory || job.checkpoint_directory}
        />
        {job.timeout_seconds !== undefined && (
          <CardInfoField
            label="Deadline"
            value={`${job.timeout_seconds}s including queue time`}
          />
        )}
      </div>
      {job.message && <p className="mt-3 text-sm break-words">{job.message}</p>}
      {showFiles && <JobFiles id={job.id} />}
    </Card>
  )
}

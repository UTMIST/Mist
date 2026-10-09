import { useState } from 'react'
import { computeNames } from '#/components/HardwarePanel.tsx'
import { JobFiles } from '#/components/jobs/JobFiles.tsx'
import { CardHeader, CardInfoField } from '#/components/Card.tsx'
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
  const [expanded, setExpanded] = useState(false)
  return (
    <article className="border-b border-gray-200 last:border-b-0">
      <button
        type="button"
        aria-expanded={expanded}
        aria-controls={`details-${job.id}`}
        aria-label={`${expanded ? 'Hide' : 'Show'} details for ${job.name}`}
        onClick={() => setExpanded(!expanded)}
        className="mist-job-summary w-full grid grid-cols-[1rem_minmax(0,1fr)_auto] md:grid-cols-[1rem_minmax(0,2fr)_1fr_1fr_1fr] items-center gap-4 px-5 py-4 text-left hover:bg-gray-50 focus-visible:outline-2 focus-visible:outline-green-700"
      >
        <span aria-hidden="true" className="text-gray-500">
          {expanded ? '⌄' : '›'}
        </span>
        <span className="min-w-0">
          <span className="block font-medium truncate">{job.name}</span>
          <span className="block text-xs text-gray-500 truncate">
            {job.creator_name || job.id}
          </span>
        </span>
        <span className="mist-job-status" data-state={job.job_state}>
          {statusNames[job.job_state]}
        </span>
        <span className="hidden md:block text-sm capitalize">
          {computeNames[job.accelerator]}
          {job.device_count ? ` × ${job.device_count}` : ''}
        </span>
        <time
          dateTime={job.created}
          className="mist-job-created hidden md:block text-sm text-gray-500"
        >
          {new Date(job.created).toLocaleString()}
        </time>
      </button>
      {expanded && (
        <div id={`details-${job.id}`} className="px-5 pb-5 pt-2 bg-gray-50/60">
          <CardHeader header="Job details">
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
            {job.can_cancel !== false &&
              (job.job_state === 'Scheduled' ||
                job.job_state === 'InProgress') && (
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
            <CardInfoField
              label="Compute"
              value={`${computeNames[job.accelerator]}${job.device_count ? ` × ${job.device_count}${job.accelerator === 'tenstorrent' ? ' boards' : ' GPUs'}` : ''}`}
            />
            <CardInfoField label="Job ID" value={job.id} />
            {job.creator_name && (
              <CardInfoField label="Submitted by" value={job.creator_name} />
            )}
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
                value={`${job.timeout_seconds}s ${job.team_id ? 'after admission' : 'including queue time'}`}
              />
            )}
          </div>
          {job.message && (
            <p className="mt-3 text-sm break-words">{job.message}</p>
          )}
          {showFiles && <JobFiles id={job.id} />}
        </div>
      )}
    </article>
  )
}

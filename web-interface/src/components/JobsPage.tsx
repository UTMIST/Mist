import { useState } from 'react'
import { jobsAPI, storageAPI } from '#/api.ts'
import type { Submission } from '#/api.ts'
import { HardwarePanel } from '#/components/HardwarePanel.tsx'
import { JobSubmissionForm } from '#/components/jobs/JobSubmissionForm.tsx'
import { JobCard } from '#/components/jobs/JobCard.tsx'
import { JobLogs } from '#/components/jobs/JobLogs.tsx'
import { errorMessage, usePolling } from '#/hooks/usePolling.ts'

export function JobsPage() {
  const jobs = usePolling(jobsAPI.list, 3000)
  const hardware = usePolling(jobsAPI.hardware)
  const datasets = usePolling(storageAPI.list, 15000)
  const images = usePolling(jobsAPI.images, 15000)
  const [selectedLogs, setSelectedLogs] = useState<string | null>(null)
  const [cancelling, setCancelling] = useState<string | null>(null)
  const [actionError, setActionError] = useState('')
  const [submitted, setSubmitted] = useState('')

  async function submit(submission: Submission) {
    const response = await jobsAPI.submit(submission)
    setSubmitted(response.job_id)
    setActionError('')
    jobs.refresh()
    hardware.refresh()
  }
  async function cancel(id: string) {
    setCancelling(id)
    setActionError('')
    try {
      await jobsAPI.cancel(id)
      jobs.refresh()
      hardware.refresh()
    } catch (err) {
      setActionError(errorMessage(err))
    } finally {
      setCancelling(null)
    }
  }

  return (
    <div className="px-6 py-8 lg:px-16 space-y-6">
      <h1 className="text-2xl font-bold">Jobs</h1>
      <HardwarePanel
        hardware={hardware.data}
        error={hardware.error}
        loading={hardware.loading}
      />
      {images.error && (
        <p role="alert" className="text-red-700">
          Image catalog unavailable: {images.error}
        </p>
      )}
      <JobSubmissionForm
        catalog={images.error ? null : images.data}
        hardware={hardware.error ? null : hardware.data}
        datasets={datasets.data?.datasets ?? []}
        datasetError={datasets.error}
        onSubmit={submit}
      />
      {submitted && (
        <p role="status" className="text-green-800">
          Submitted job <code>{submitted}</code>.
        </p>
      )}
      {(actionError || jobs.error) && (
        <p role="alert" className="text-red-700">
          {actionError || jobs.error}
        </p>
      )}
      {jobs.loading && <p>Loading jobs…</p>}
      {!jobs.loading && !jobs.error && jobs.data?.jobs.length === 0 && (
        <p>No jobs yet. Submit one above.</p>
      )}
      {selectedLogs && (
        <JobLogs
          key={selectedLogs}
          id={selectedLogs}
          onClose={() => setSelectedLogs(null)}
        />
      )}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        {jobs.data?.jobs.map((job) => (
          <JobCard
            key={job.id}
            job={job}
            cancelling={cancelling !== null}
            onLogs={() => setSelectedLogs(job.id)}
            onCancel={() => {
              void cancel(job.id)
            }}
          />
        ))}
      </div>
    </div>
  )
}

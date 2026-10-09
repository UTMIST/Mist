import { Picker } from '#/components/Picker.tsx'
import { useState } from 'react'
import {
  History,
  Search,
  ChevronLeft,
  ChevronRight,
  CheckCircle2,
  Clock3,
  Activity,
  Plus,
} from 'lucide-react'
import { Modal } from '#/components/Modal.tsx'
import { PanelHeading } from '#/components/Card.tsx'
import { jobsAPI, storageAPI } from '#/api.ts'
import type { Submission } from '#/api.ts'
import { HardwarePanel } from '#/components/HardwarePanel.tsx'
import { JobSubmissionForm } from '#/components/jobs/JobSubmissionForm.tsx'
import { JobCard } from '#/components/jobs/JobCard.tsx'
import { JobLogs } from '#/components/jobs/JobLogs.tsx'
import { errorMessage, usePolling } from '#/hooks/usePolling.ts'
import { useTeam } from '#/teams.tsx'

export function JobsPage() {
  const { team } = useTeam()
  const jobs = usePolling(jobsAPI.list, 3000)
  const hardware = usePolling(jobsAPI.hardware)
  const datasets = usePolling(storageAPI.list, 15000)
  const images = usePolling(jobsAPI.images, 15000)
  const [selectedLogs, setSelectedLogs] = useState<string | null>(null)
  const [cancelling, setCancelling] = useState<string | null>(null)
  const [actionError, setActionError] = useState('')
  const [composing, setComposing] = useState(false)
  const [submitted, setSubmitted] = useState('')
  const [search, setSearch] = useState('')
  const [status, setStatus] = useState('all')
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  const filtered = (jobs.data?.jobs ?? []).filter(
    (job) =>
      (status === 'all' || job.job_state === status) &&
      `${job.name} ${job.id} ${job.creator_name ?? ''}`
        .toLowerCase()
        .includes(search.trim().toLowerCase()),
  )
  const pageCount = Math.max(1, Math.ceil(filtered.length / pageSize))
  const currentPage = Math.min(page, pageCount)
  const start = (currentPage - 1) * pageSize
  const visibleJobs = filtered.slice(start, start + pageSize)
  function filterStatus(next: string) {
    setStatus(next)
    setPage(1)
    setSelectedLogs(null)
  }
  function changePage(next: number) {
    setPage(next)
    setSelectedLogs(null)
  }

  async function submit(submission: Submission) {
    const response = await jobsAPI.submit(submission)
    setSubmitted(response.job_id)
    setComposing(false)
    setPage(1)
    setSearch('')
    setStatus('all')
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
      <div className="mist-page-heading">
        <div>
          <p className="mist-eyebrow">{team?.name ?? 'Legacy workspace'}</p>
          <h1 className="text-2xl font-bold">Jobs</h1>
          <p className="mist-description">
            From an experiment to a result. Run your code on the right hardware.
          </p>
        </div>
        <button
          type="button"
          className="mist-button mist-button-primary"
          onClick={() => setComposing(true)}
        >
          <Plus size={16} aria-hidden="true" /> New job
        </button>
      </div>
      <div
        className="mist-metrics mist-clickable-metrics"
        aria-label="Filter jobs by status"
      >
        {(
          [
            [
              'Running',
              jobs.data?.jobs.filter((j) => j.job_state === 'InProgress')
                .length,
              'InProgress',
              Activity,
              Plus,
            ],
            [
              'In queue',
              jobs.data?.jobs.filter((j) => j.job_state === 'Scheduled').length,
              'Scheduled',
              Clock3,
            ],
            [
              'Completed',
              jobs.data?.jobs.filter((j) => j.job_state === 'Success').length,
              'Success',
              CheckCircle2,
            ],
          ] as const
        ).map(([label, count, state, Icon]) => (
          <button
            type="button"
            key={label}
            aria-pressed={status === state}
            onClick={() => filterStatus(status === state ? 'all' : state)}
          >
            <span>
              <Icon size={15} aria-hidden="true" /> {label}{' '}
              <ChevronRight size={14} aria-hidden="true" />
            </span>
            <strong>{count ?? '—'}</strong>
          </button>
        ))}
      </div>
      <details className="mist-hardware-details">
        <summary>
          <span>Connected compute</span>
          <span>
            {hardware.data?.pools
              .map(
                (pool) =>
                  `${pool.available ?? '—'} ${pool.accelerator === 'nvidia' ? 'GPUs' : 'TT boards'}`,
              )
              .join(' · ') ?? 'Checking availability…'}
          </span>
        </summary>
        <HardwarePanel
          hardware={hardware.data}
          error={hardware.error}
          loading={hardware.loading}
        />
      </details>
      {images.error && (
        <p role="alert" className="text-red-700">
          Image catalog unavailable: {images.error}
        </p>
      )}
      {composing && (
        <Modal title="New job" onClose={() => setComposing(false)}>
          <JobSubmissionForm
            catalog={images.error ? null : images.data}
            hardware={hardware.error ? null : hardware.data}
            datasets={datasets.data?.datasets ?? []}
            datasetError={datasets.error}
            onSubmit={submit}
          />
        </Modal>
      )}
      {submitted && (
        <p role="status" className="text-green-800">
          Job submitted. Track it in the history below.
        </p>
      )}
      <div>
        <div className="min-w-0 space-y-4">
          {(actionError || jobs.error) && (
            <p role="alert" className="text-red-700">
              {actionError || jobs.error}
            </p>
          )}
          {jobs.loading && <p>Loading jobs…</p>}
          {selectedLogs && (
            <JobLogs
              key={selectedLogs}
              id={selectedLogs}
              onClose={() => setSelectedLogs(null)}
            />
          )}
          <section
            aria-label="Job history"
            className="mist-history-panel border border-gray-200 rounded-xl overflow-hidden"
          >
            <PanelHeading
              title="Job history"
              description="Track every experiment. Select a row to see its details."
              icon={<History size={20} />}
              action={
                <span className="mist-badge">
                  {jobs.data?.count ?? '—'} jobs
                </span>
              }
            />
            <div className="mist-history-toolbar">
              <div className="mist-search-field">
                <Search size={16} aria-hidden="true" />
                <input
                  aria-label="Search job history"
                  placeholder="Search jobs, people or ID…"
                  value={search}
                  onChange={(e) => {
                    setSearch(e.target.value)
                    setPage(1)
                  }}
                />
              </div>
              <Picker
                aria-label="Filter job status"
                value={status}
                onChange={(e) => filterStatus(e.target.value)}
              >
                <option value="all">All statuses</option>
                <option value="Scheduled">Waiting</option>
                <option value="InProgress">Running</option>
                <option value="Success">Completed</option>
                <option value="Failure">Failed</option>
                <option value="Cancelled">Cancelled</option>
              </Picker>
            </div>
            <div className="mist-history-labels hidden md:grid grid-cols-[1rem_minmax(0,2fr)_1fr_1fr_1fr] gap-4 px-5 py-3 text-xs text-gray-500 bg-gray-50 border-b">
              <span />
              <span>Job / submitted by</span>
              <span>Status</span>
              <span>Compute</span>
              <span className="mist-job-created">Created</span>
            </div>
            {visibleJobs.map((job) => (
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
            {!jobs.loading && !jobs.error && filtered.length === 0 && (
              <div className="mist-empty-state">
                <History size={26} aria-hidden="true" />
                <h3>
                  {search || status !== 'all'
                    ? 'No matching jobs'
                    : 'Your first experiment starts here'}
                </h3>
                <p>
                  {search || status !== 'all'
                    ? 'Try a different search or status.'
                    : 'Choose your compute and submit a job. Its progress and results will appear here.'}
                </p>
                {(search || status !== 'all') && (
                  <button
                    type="button"
                    onClick={() => {
                      setSearch('')
                      filterStatus('all')
                    }}
                  >
                    Clear filters
                  </button>
                )}
              </div>
            )}
            <nav
              className="mist-pagination"
              aria-label="Job history pagination"
            >
              <div>
                <label htmlFor="jobs-page-size">Rows</label>
                <Picker
                  id="jobs-page-size"
                  value={pageSize}
                  onChange={(e) => {
                    setPageSize(Number(e.target.value))
                    setPage(1)
                  }}
                >
                  {[10, 25, 50].map((n) => (
                    <option key={n} value={n}>
                      {n}
                    </option>
                  ))}
                </Picker>
              </div>
              <span role="status">
                {filtered.length
                  ? `${start + 1}–${Math.min(start + pageSize, filtered.length)} of ${filtered.length}`
                  : '0 jobs'}{' '}
                · Page {currentPage} of {pageCount}
              </span>
              <div>
                <button
                  type="button"
                  aria-label="Previous page"
                  disabled={currentPage <= 1}
                  onClick={() => changePage(currentPage - 1)}
                >
                  <ChevronLeft size={16} aria-hidden="true" />
                  <span>Previous</span>
                </button>
                <button
                  type="button"
                  aria-label="Next page"
                  disabled={currentPage >= pageCount}
                  onClick={() => changePage(currentPage + 1)}
                >
                  <span>Next</span>
                  <ChevronRight size={16} aria-hidden="true" />
                </button>
              </div>
            </nav>
          </section>
        </div>
      </div>
    </div>
  )
}

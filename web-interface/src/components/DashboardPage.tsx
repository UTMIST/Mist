import { Link } from '@tanstack/react-router'
import { useState } from 'react'
import { jobsAPI } from '#/api.ts'
import type { Job } from '#/api.ts'
import { useAccount } from '#/auth.tsx'
import { useWorkspace } from '#/teams.tsx'
import { usePolling } from '#/hooks/usePolling.ts'
import { HardwarePanel, computeNames } from '#/components/HardwarePanel.tsx'
import { Modal } from '#/components/Modal.tsx'
import {
  Server,
  Activity,
  Layers,
  Clock3,
  Cpu,
  User,
  ExternalLink,
  Terminal,
  Info,
} from 'lucide-react'

const statusNames: Record<string, string> = {
  Scheduled: 'Queued / Waiting',
  InProgress: 'Running',
  Success: 'Completed',
  Failure: 'Failed',
  Cancelled: 'Cancelled',
}

function OverviewJobRow({
  job,
  isOwn,
  onInspect,
}: {
  job: Job
  isOwn: boolean
  onInspect: () => void
}) {
  const isRunning = job.job_state === 'InProgress'
  const isQueued = job.job_state === 'Scheduled'

  return (
    <div
      className={`border rounded-lg p-4 transition-colors ${
        isOwn
          ? 'bg-blue-50/40 border-blue-200'
          : 'bg-white border-gray-200 hover:border-gray-300'
      }`}
    >
      <div className="flex flex-wrap items-center justify-between gap-3 mb-2">
        <div className="flex items-center gap-2 min-w-0">
          <span
            className={`inline-flex items-center px-2 py-0.5 rounded text-xs font-semibold ${
              isRunning
                ? 'bg-green-100 text-green-800'
                : isQueued
                  ? 'bg-amber-100 text-amber-800'
                  : 'bg-gray-100 text-gray-700'
            }`}
          >
            {isRunning && (
              <span className="w-1.5 h-1.5 rounded-full bg-green-600 mr-1.5 animate-pulse" />
            )}
            {statusNames[job.job_state] ?? job.job_state}
          </span>
          <span className="font-semibold truncate text-gray-900">{job.name}</span>
          <span className="text-xs text-gray-500 font-mono truncate">{job.id}</span>
        </div>
        <div className="flex items-center gap-2">
          {isOwn && (
            <span className="text-xs bg-blue-100 text-blue-800 font-medium px-2 py-0.5 rounded">
              Your Job
            </span>
          )}
          <button
            type="button"
            onClick={onInspect}
            className="text-xs border border-gray-300 hover:bg-gray-100 text-gray-700 px-2.5 py-1 rounded inline-flex items-center gap-1 font-medium cursor-pointer"
          >
            <Info size={12} /> Details & Logs
          </button>
        </div>
      </div>

      <div className="grid grid-cols-2 sm:grid-cols-4 gap-2 text-xs text-gray-600 pt-1 border-t border-gray-100">
        <div>
          <span className="text-gray-400 block">Machine:</span>
          <span className="font-medium text-gray-800">
            {job.node ? `Node ${job.node}` : 'Waiting for placement'}
          </span>
        </div>
        <div>
          <span className="text-gray-400 block">Compute:</span>
          <span className="font-medium text-gray-800">
            {computeNames[job.accelerator]}
            {job.device_count ? ` × ${job.device_count}` : ''}
          </span>
        </div>
        <div>
          <span className="text-gray-400 block">Attribution / User:</span>
          <span className="font-medium text-gray-800 truncate block">
            {job.creator_name || 'Anonymous'}
            {job.team_id ? ` · ${job.team_id}` : ''}
          </span>
        </div>
        <div>
          <span className="text-gray-400 block">Submitted:</span>
          <span className="font-medium text-gray-800">
            {new Date(job.created).toLocaleTimeString([], {
              hour: '2-digit',
              minute: '2-digit',
            })}
          </span>
        </div>
      </div>

      {job.message && isQueued && (
        <div className="mt-2 text-xs text-amber-800 bg-amber-50 rounded px-2.5 py-1 border border-amber-200">
          ℹ️ {job.message}
        </div>
      )}
    </div>
  )
}

export function DashboardPage() {
  const { user } = useAccount()
  const { workspace } = useWorkspace()
  const hardware = usePolling(jobsAPI.hardware, 5000)
  const jobs = usePolling(jobsAPI.listCluster, 5000)

  const [inspectingJob, setInspectingJob] = useState<Job | null>(null)
  const [logs, setLogs] = useState<string | null>(null)
  const [loadingLogs, setLoadingLogs] = useState(false)

  const allJobs = jobs.data?.jobs ?? []
  const activeJobs = allJobs.filter(
    (j) => j.job_state === 'InProgress' || j.job_state === 'Scheduled',
  )
  const completedJobs = allJobs.filter(
    (j) => j.job_state !== 'InProgress' && j.job_state !== 'Scheduled',
  )

  const myActiveJobs = activeJobs.filter((j) => j.creator_id === user?.id)
  const otherActiveJobs = activeJobs.filter((j) => j.creator_id !== user?.id)
  const myCompletedJobs = completedJobs.filter((j) => j.creator_id === user?.id)

  const totalNvidia =
    hardware.data?.pools
      .filter((p) => p.accelerator === 'nvidia')
      .reduce((acc, p) => acc + p.total, 0) ?? 0
  const allocatedNvidia =
    hardware.data?.pools
      .filter((p) => p.accelerator === 'nvidia')
      .reduce((acc, p) => acc + p.allocated, 0) ?? 0

  const totalTT =
    hardware.data?.pools
      .filter((p) => p.accelerator === 'tenstorrent')
      .reduce((acc, p) => acc + p.total, 0) ?? 0
  const allocatedTT =
    hardware.data?.pools
      .filter((p) => p.accelerator === 'tenstorrent')
      .reduce((acc, p) => acc + p.allocated, 0) ?? 0

  const readyMachines =
    hardware.data?.machines.filter((m) => m.ready).length ?? 0
  const totalMachines = hardware.data?.machines.length ?? 0

  async function openInspect(job: Job) {
    setInspectingJob(job)
    setLogs(null)
    setLoadingLogs(true)
    try {
      const resp = await jobsAPI.logs(job.id)
      setLogs(resp.logs || '(No log output generated yet)')
    } catch {
      setLogs('(Failed to retrieve logs)')
    } finally {
      setLoadingLogs(false)
    }
  }

  return (
    <main className="px-6 py-8 lg:px-16 space-y-8">
      {/* Technical Header */}
      <div className="mist-page-heading">
        <div>
          <div className="flex items-center gap-2">
            <p className="mist-eyebrow">
              {workspace ? `${workspace.name} (${workspace.type ?? 'team'})` : 'Cluster Overview'}
            </p>
          </div>
          <h1 className="text-2xl font-bold tracking-tight">System Overview</h1>
          <p className="mist-description">
            Live compute availability, hardware load, and active workload queue.
          </p>
        </div>
        <div className="flex items-center gap-3">
          <span className="mist-live">
            <span />
            Cluster live
          </span>
          <Link
            to="/jobs"
            className="text-xs border rounded px-3 py-1.5 hover:bg-gray-50 font-medium inline-flex items-center gap-1 text-gray-700"
          >
            Manage Jobs <ExternalLink size={12} />
          </Link>
        </div>
      </div>

      {/* Cluster Hardware Load Summary */}
      <section aria-labelledby="cluster-metrics-heading">
        <h2 id="cluster-metrics-heading" className="sr-only">
          Cluster load metrics
        </h2>
        <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
          <div className="border border-gray-200 rounded-lg p-4 bg-white shadow-xs">
            <div className="flex items-center justify-between text-gray-500 text-xs font-semibold uppercase tracking-wider mb-1">
              <span>Connected Nodes</span>
              <Server size={15} />
            </div>
            <div className="text-2xl font-bold text-gray-900">
              {readyMachines} / {totalMachines}
            </div>
            <p className="text-xs text-gray-500 mt-1">Ready for scheduling</p>
          </div>

          <div className="border border-gray-200 rounded-lg p-4 bg-white shadow-xs">
            <div className="flex items-center justify-between text-gray-500 text-xs font-semibold uppercase tracking-wider mb-1">
              <span>NVIDIA GPUs</span>
              <Activity size={15} />
            </div>
            <div className="text-2xl font-bold text-gray-900">
              {allocatedNvidia} / {totalNvidia}{' '}
              <span className="text-xs font-normal text-gray-500">
                ({totalNvidia > 0 ? Math.round((allocatedNvidia / totalNvidia) * 100) : 0}%)
              </span>
            </div>
            <div className="w-full bg-gray-200 h-1.5 rounded-full mt-2 overflow-hidden">
              <div
                className="bg-green-600 h-1.5 rounded-full transition-all"
                style={{
                  width: `${totalNvidia > 0 ? Math.min(100, (allocatedNvidia / totalNvidia) * 100) : 0}%`,
                }}
              />
            </div>
          </div>

          <div className="border border-gray-200 rounded-lg p-4 bg-white shadow-xs">
            <div className="flex items-center justify-between text-gray-500 text-xs font-semibold uppercase tracking-wider mb-1">
              <span>Tenstorrent Boards</span>
              <Cpu size={15} />
            </div>
            <div className="text-2xl font-bold text-gray-900">
              {allocatedTT} / {totalTT}{' '}
              <span className="text-xs font-normal text-gray-500">
                ({totalTT > 0 ? Math.round((allocatedTT / totalTT) * 100) : 0}%)
              </span>
            </div>
            <div className="w-full bg-gray-200 h-1.5 rounded-full mt-2 overflow-hidden">
              <div
                className="bg-blue-600 h-1.5 rounded-full transition-all"
                style={{
                  width: `${totalTT > 0 ? Math.min(100, (allocatedTT / totalTT) * 100) : 0}%`,
                }}
              />
            </div>
          </div>

          <div className="border border-gray-200 rounded-lg p-4 bg-white shadow-xs">
            <div className="flex items-center justify-between text-gray-500 text-xs font-semibold uppercase tracking-wider mb-1">
              <span>Active Workloads</span>
              <Clock3 size={15} />
            </div>
            <div className="text-2xl font-bold text-gray-900">
              {activeJobs.length}
            </div>
            <p className="text-xs text-gray-500 mt-1">
              {activeJobs.filter((j) => j.job_state === 'InProgress').length} running ·{' '}
              {activeJobs.filter((j) => j.job_state === 'Scheduled').length} queued
            </p>
          </div>
        </div>
      </section>

      {/* Machine Details & Load */}
      <section className="space-y-3">
        <div className="flex items-center justify-between">
          <h2 className="text-lg font-semibold text-gray-900 flex items-center gap-2">
            <Server size={18} />
            Machine Status & Hardware Availability
          </h2>
          <span className="text-xs text-gray-500">Live placement availability</span>
        </div>
        <HardwarePanel
          hardware={hardware.data}
          error={hardware.error}
          loading={hardware.loading}
        />
        <p className="text-xs text-gray-500">
          CPU and RAM figures are allocatable scheduling capacity, not real-time OS utilization.
          Jobs reserve whole NVIDIA GPUs or whole Tenstorrent boards.
        </p>
      </section>

      {/* Current Jobs Section: First User's, Then Everyone Else's */}
      <section className="space-y-6">
        <div className="border-b pb-3">
          <div className="flex items-center justify-between">
            <div>
              <h2 className="text-lg font-semibold text-gray-900 flex items-center gap-2">
                <Activity size={18} />
                Cluster Workloads & Queue
              </h2>
              <p className="text-xs text-gray-500 mt-0.5">
                Current execution status across machines. Your jobs are shown first to track queue progression.
              </p>
            </div>
            <span className="text-xs bg-gray-100 text-gray-700 px-2.5 py-1 rounded font-medium">
              Read-Only Monitor
            </span>
          </div>
        </div>

        {jobs.error && (
          <p role="alert" className="text-red-700 text-sm">
            Workload queue unavailable: {jobs.error}
          </p>
        )}

        {/* 1. Logged-in User's Active Jobs */}
        <div className="space-y-3">
          <div className="flex items-center justify-between">
            <h3 className="text-sm font-bold uppercase tracking-wider text-blue-900 flex items-center gap-1.5">
              <User size={14} />
              Your Active Experiments ({myActiveJobs.length})
            </h3>
            {myActiveJobs.length > 0 && (
              <span className="text-xs text-blue-700">
                {myActiveJobs.some((j) => j.job_state === 'InProgress')
                  ? 'Active run in progress'
                  : 'Waiting in cluster queue'}
              </span>
            )}
          </div>

          {myActiveJobs.length > 0 ? (
            <div className="space-y-2">
              {myActiveJobs.map((job) => (
                <OverviewJobRow
                  key={job.id}
                  job={job}
                  isOwn={true}
                  onInspect={() => void openInspect(job)}
                />
              ))}
            </div>
          ) : (
            <div className="border border-dashed border-gray-200 rounded-lg p-5 text-center text-sm text-gray-500 bg-gray-50/50">
              You have no jobs currently running or queued.
              {myCompletedJobs.length > 0 && (
                <span className="block text-xs text-gray-400 mt-1">
                  You have {myCompletedJobs.length} completed job(s) in history. Visit Jobs to view them.
                </span>
              )}
            </div>
          )}
        </div>

        {/* 2. Everyone Else's Active Jobs */}
        <div className="space-y-3 pt-4 border-t border-gray-100">
          <div className="flex items-center justify-between">
            <h3 className="text-sm font-bold uppercase tracking-wider text-gray-700 flex items-center gap-1.5">
              <Layers size={14} />
              Other Cluster Workloads ({otherActiveJobs.length})
            </h3>
            <span className="text-xs text-gray-500">
              {otherActiveJobs.filter((j) => j.job_state === 'InProgress').length} executing ·{' '}
              {otherActiveJobs.filter((j) => j.job_state === 'Scheduled').length} queued
            </span>
          </div>

          {otherActiveJobs.length > 0 ? (
            <div className="space-y-2">
              {otherActiveJobs.map((job) => (
                <OverviewJobRow
                  key={job.id}
                  job={job}
                  isOwn={false}
                  onInspect={() => void openInspect(job)}
                />
              ))}
            </div>
          ) : (
            <div className="border border-dashed border-gray-200 rounded-lg p-5 text-center text-sm text-gray-500 bg-gray-50/50">
              No other active experiments on the cluster right now. Compute machines have available capacity.
            </div>
          )}
        </div>

        {/* 3. Recent Finished Jobs (Summary) */}
        {completedJobs.length > 0 && (
          <details className="pt-2 text-sm text-gray-600">
            <summary className="cursor-pointer font-medium text-gray-700 hover:text-gray-900 select-none py-2">
              Recent Completed Workloads ({completedJobs.length})
            </summary>
            <div className="space-y-2 mt-2">
              {completedJobs.slice(0, 10).map((job) => (
                <OverviewJobRow
                  key={job.id}
                  job={job}
                  isOwn={job.creator_id === user?.id}
                  onInspect={() => void openInspect(job)}
                />
              ))}
              {completedJobs.length > 10 && (
                <p className="text-xs text-center text-gray-500 pt-2">
                  Showing 10 most recent. <Link to="/jobs" className="text-accent">View all in Jobs →</Link>
                </p>
              )}
            </div>
          </details>
        )}
      </section>

      {/* Read-Only Inspection Modal */}
      {inspectingJob && (
        <Modal
          title={`Job Details: ${inspectingJob.name || inspectingJob.id}`}
          onClose={() => setInspectingJob(null)}
        >
          <div className="space-y-4 max-h-[80vh] overflow-y-auto pr-1">
            <div className="grid grid-cols-2 gap-3 text-xs bg-gray-50 p-3 rounded border">
              <div>
                <span className="text-gray-500 block">Job ID:</span>
                <span className="font-mono font-medium">{inspectingJob.id}</span>
              </div>
              <div>
                <span className="text-gray-500 block">Status:</span>
                <span className="font-semibold text-gray-800">
                  {statusNames[inspectingJob.job_state] ?? inspectingJob.job_state}
                </span>
              </div>
              <div>
                <span className="text-gray-500 block">Compute:</span>
                <span className="font-medium">
                  {computeNames[inspectingJob.accelerator]}
                  {inspectingJob.device_count ? ` × ${inspectingJob.device_count}` : ''}
                </span>
              </div>
              <div>
                <span className="text-gray-500 block">Placed Node:</span>
                <span className="font-medium">
                  {inspectingJob.node ? `Node ${inspectingJob.node}` : 'Waiting for placement'}
                </span>
              </div>
              <div>
                <span className="text-gray-500 block">Submitted By:</span>
                <span className="font-medium">{inspectingJob.creator_name || 'Anonymous'}</span>
              </div>
              <div>
                <span className="text-gray-500 block">Workspace:</span>
                <span className="font-medium">{inspectingJob.team_id || 'Unassigned'}</span>
              </div>
            </div>

            <div>
              <div className="flex items-center justify-between mb-1.5">
                <span className="text-xs font-bold uppercase tracking-wider text-gray-600 flex items-center gap-1">
                  <Terminal size={13} />
                  Console Logs (Read-Only)
                </span>
                {loadingLogs && <span className="text-xs text-gray-500">Loading output…</span>}
              </div>
              <pre className="p-3 bg-gray-900 text-green-400 font-mono text-xs rounded-lg max-h-72 overflow-y-auto whitespace-pre-wrap break-all">
                {loadingLogs ? 'Connecting to pod logs…' : logs}
              </pre>
            </div>

            <p className="text-xs text-gray-500 italic">
              Note: The overview monitor is read-only. To submit new experiments or cancel active workloads, visit the Jobs tab.
            </p>
          </div>
        </Modal>
      )}
    </main>
  )
}
export const Dashboard = DashboardPage

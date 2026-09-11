import { createFileRoute } from '@tanstack/react-router'
import { useState } from 'react'
import { Play, Terminal, CheckCircle2, LoaderCircle } from 'lucide-react'
import { HardwarePanel } from '#/components/HardwarePanel'
import { api, useHardware, useJobs } from '#/localCompute'
import type { ComputeJob } from '#/localCompute'

export const Route = createFileRoute('/jobs')({ component: JobsPage })

function JobResult({ job }: { job: ComputeJob }) {
  const running =
    job.job_state === 'Scheduled' || job.job_state === 'InProgress'
  const statusColor =
    job.job_state === 'Success'
      ? 'bg-emerald-50 text-emerald-700'
      : job.job_state === 'Failure'
        ? 'bg-red-50 text-red-700'
        : 'bg-blue-50 text-blue-700'
  return (
    <article className="rounded-2xl border border-slate-200 bg-white p-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h3 className="font-bold text-slate-900">
            Matrix multiplication · {job.payload.matrix_size ?? 2048} ×{' '}
            {job.payload.matrix_size ?? 2048}
          </h3>
          <p className="mt-1 text-sm text-slate-500">
            {new Date(job.created).toLocaleString()}
          </p>
        </div>
        <span
          className={
            'flex items-center gap-2 rounded-full px-3 py-1 text-sm font-semibold ' +
            statusColor
          }
        >
          {running ? (
            <LoaderCircle size={15} className="animate-spin" />
          ) : job.job_state === 'Success' ? (
            <CheckCircle2 size={15} />
          ) : null}
          {job.job_state === 'InProgress'
            ? 'Running on GPU'
            : job.job_state === 'Scheduled'
              ? 'Queued'
              : job.job_state === 'Success'
                ? 'Completed'
                : job.job_state}
        </span>
      </div>
      {running && (
        <p className="mt-4 text-sm text-slate-600">
          {job.job_state === 'Scheduled'
            ? 'Waiting for the local worker.'
            : 'Measuring CUDA and CPU performance, then checking that the results match.'}
        </p>
      )}
      {job.error && (
        <p
          role="alert"
          className="mt-4 rounded-lg bg-red-50 p-3 text-sm text-red-700"
        >
          {job.error}
        </p>
      )}
      {job.result && (
        <>
          <div className="my-6 grid grid-cols-2 gap-5 sm:grid-cols-4">
            {[
              ['GPU time', job.result.gpu_ms + ' ms'],
              ['CPU time', job.result.cpu_ms + ' ms'],
              ['Speedup', job.result.speedup + '×'],
              ['Peak tensor VRAM', job.result.peak_vram_mib + ' MiB'],
            ].map(([label, value]) => (
              <div key={label}>
                <p className="text-xs font-medium text-slate-500">{label}</p>
                <p className="mt-1 text-2xl font-bold tracking-tight">
                  {value}
                </p>
              </div>
            ))}
          </div>
          <p className="text-sm text-slate-600">
            {job.result.gpu} · FP32 · {job.result.gpu_iterations} GPU iterations
            / {job.result.cpu_iterations} CPU iterations with{' '}
            {job.result.cpu_threads} CPU threads. Timings exclude data transfer.
            This measures this workload, not overall machine performance.
          </p>
        </>
      )}
      {job.logs && (
        <details className="mt-5">
          <summary className="cursor-pointer text-sm font-semibold text-slate-700">
            View execution output
          </summary>
          <pre className="mt-3 max-h-72 overflow-auto rounded-xl bg-slate-950 p-4 text-xs leading-6 text-slate-200">
            {job.logs}
          </pre>
        </details>
      )}
      <p className="mt-4 break-all font-mono text-xs text-slate-400">
        {job.id}
      </p>
    </article>
  )
}

function JobsPage() {
  const { hardware, error: hardwareError } = useHardware()
  const { jobs, error: jobsError, refresh } = useJobs()
  const [size, setSize] = useState(2048)
  const [submitting, setSubmitting] = useState(false)
  const [submitError, setSubmitError] = useState('')
  const busy = jobs.some((job) =>
    ['Scheduled', 'InProgress'].includes(job.job_state),
  )

  async function submit() {
    setSubmitting(true)
    setSubmitError('')
    try {
      await api('/jobs', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          type: 'cuda_benchmark',
          gpu: 'NVIDIA',
          payload: { matrix_size: size },
        }),
      })
      await refresh()
    } catch (error) {
      setSubmitError(
        error instanceof Error ? error.message : 'Could not submit job',
      )
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <main className="mx-auto max-w-6xl space-y-7 px-5 py-10 sm:px-8">
      <header>
        <p className="text-xs font-bold uppercase tracking-widest text-blue-600">
          Local compute
        </p>
        <h1 className="mt-2 text-3xl font-bold tracking-tight">
          Put your GPU to work.
        </h1>
        <p className="mt-3 text-slate-600">
          Run a real CUDA workload on this machine and inspect the measured
          results.
        </p>
      </header>
      <HardwarePanel hardware={hardware} error={hardwareError} />
      <section className="rounded-2xl border border-blue-100 bg-blue-50/50 p-6 sm:p-8">
        <div className="flex items-center gap-3">
          <Terminal className="text-blue-600" size={22} />
          <h2 className="text-lg font-bold">CPU vs GPU benchmark</h2>
        </div>
        <p className="mt-3 max-w-3xl text-sm leading-6 text-slate-600">
          Multiply two matrices with PyTorch on CUDA and on the CPU. Each job
          measures both devices, verifies the answers, and saves the output. No
          model downloads needed.
        </p>
        <div className="mt-6 flex flex-wrap items-end gap-4">
          <label className="text-sm font-semibold text-slate-700">
            Matrix size
            <select
              className="mt-2 block rounded-lg border border-slate-300 bg-white px-4 py-2.5"
              value={size}
              onChange={(event) => setSize(Number(event.target.value))}
            >
              {[1024, 2048, 4096].map((n) => (
                <option key={n} value={n}>
                  {n} × {n}
                </option>
              ))}
            </select>
          </label>
          <button
            onClick={submit}
            disabled={
              submitting ||
              busy ||
              !hardware?.available ||
              !!hardwareError ||
              !!jobsError
            }
            className="flex items-center gap-2 rounded-lg bg-blue-600 px-5 py-3 text-sm font-bold text-white transition hover:bg-blue-700 disabled:cursor-not-allowed disabled:bg-slate-300"
          >
            <Play size={16} />
            {submitting
              ? 'Submitting…'
              : busy
                ? 'Job in progress…'
                : 'Run GPU benchmark'}
          </button>
        </div>
        {submitError && (
          <p role="alert" className="mt-4 text-sm text-red-700">
            {submitError}
          </p>
        )}
      </section>
      <section
        className="space-y-4"
        aria-label="Job history"
        aria-live="polite"
      >
        <div className="flex items-center justify-between">
          <h2 className="text-xl font-bold">Recent runs</h2>
          <span className="text-xs text-slate-500">
            Latest 50 jobs · updates automatically
          </span>
        </div>
        {jobsError && (
          <p
            role="alert"
            className="rounded-xl bg-red-50 p-4 text-sm text-red-700"
          >
            {jobsError}
          </p>
        )}
        {jobs.length === 0 && !jobsError && (
          <div className="rounded-2xl border border-dashed border-slate-300 p-10 text-center text-sm text-slate-500">
            No runs yet. Start a benchmark to see your GPU’s results here.
          </div>
        )}
        {jobs.map((job) => (
          <JobResult key={job.id} job={job} />
        ))}
      </section>
    </main>
  )
}

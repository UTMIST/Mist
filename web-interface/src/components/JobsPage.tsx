import { useEffect, useState } from 'react'
import Card, { CardHeader, CardInfoField } from '#/components/Card.tsx'
import { Button } from '#/components/Buttons.tsx'
import { jobsAPI } from '#/api.ts'
import type { Compute, Job } from '#/api.ts'

const statusNames = { Scheduled: 'Waiting', InProgress: 'Running', Success: 'Completed', Failure: 'Failed', Cancelled: 'Cancelled' }
const inputClass = 'w-full rounded border border-gray-300 bg-transparent p-2'

export function JobsPage() {
  const [jobs, setJobs] = useState<Job[]>([])
  const [error, setError] = useState('')
  const [pollError, setPollError] = useState('')
  const [logsError, setLogsError] = useState('')
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)
  const [name, setName] = useState('')
  const [compute, setCompute] = useState<Compute>('cpu')
  const [devices, setDevices] = useState(1)
  const [mode, setMode] = useState<'command' | 'training-smoke'>('command')
  const [script, setScript] = useState("print('Hello from Mist', flush=True)\n")
  const [selectedLogs, setSelectedLogs] = useState<string | null>(null)
  const [logs, setLogs] = useState('')

  async function refresh() {
    const response = await jobsAPI.list()
    setJobs(response.jobs); setLoading(false)
  }
  useEffect(() => {
    let active = true
    let fetching = false
    async function poll() {
      if (fetching) return
      fetching = true
      try {
        const response = await jobsAPI.list()
        if (active) { setJobs(response.jobs); setLoading(false); setPollError('') }
      } catch (err) { if (active) { setPollError(String(err)); setLoading(false) } }
      finally { fetching = false }
    }
    void poll()
    const interval = setInterval(() => { void poll() }, 3000)
    return () => { active = false; clearInterval(interval) }
  }, [])
  useEffect(() => {
    setLogsError('')
    if (!selectedLogs) return
    let active = true
    const id = selectedLogs
    let fetching = false
    async function poll() {
      if (fetching) return
      fetching = true
      try {
        const response = await jobsAPI.logs(id)
        if (active) { setLogs(response.logs); setLogsError('') }
      } catch (err) { if (active) setLogsError(String(err)) }
      finally { fetching = false }
    }
    void poll()
    const interval = setInterval(() => { void poll() }, 3000)
    return () => { active = false; clearInterval(interval) }
  }, [selectedLogs])
  async function submit() {
    setBusy(true); setError('')
    try {
      await jobsAPI.submit({ name, type: mode, accelerator: compute,
        device_count: compute === 'cpu' ? 0 : devices,
        ...(mode === 'command' ? { script, script_name: 'script.py' } : {}),
      })
      await refresh()
    } catch (err) { setError(String(err)) }
    finally { setBusy(false) }
  }
  async function cancel(id: string) {
    setBusy(true); setError('')
    try { await jobsAPI.cancel(id); await refresh() }
    catch (err) { setError(String(err)) }
    finally { setBusy(false) }
  }
  const invalidDevices = compute !== 'cpu' && (!Number.isInteger(devices) || devices < 1 || devices > (compute === 'tenstorrent' ? 4 : 2))
  return (
    <div className="px-6 py-8 lg:px-16 space-y-6">
      <h1 className="text-2xl font-bold">Jobs</h1>
      <Card>
        <h2 className="text-lg font-semibold mb-4">Submit a job</h2>
        <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
          <div className="text-sm"><label htmlFor="job-name">Name</label><input id="job-name" className={inputClass} value={name} onChange={(e) => setName(e.target.value)} placeholder="Optional job name" /></div>
          <div className="text-sm"><label htmlFor="job-compute">Compute</label><select id="job-compute" className={inputClass} value={compute} onChange={(e) => {
            const next = e.target.value as Compute
            setCompute(next); setDevices(1); if (next === 'cpu') setMode('command')
          }}><option value="cpu">CPU</option><option value="nvidia">NVIDIA</option><option value="tenstorrent">Tenstorrent</option></select></div>
          {compute !== 'cpu' && <div className="text-sm"><label htmlFor="job-devices">{compute === 'tenstorrent' ? 'Boards (two chips each)' : 'GPUs'}</label><input id="job-devices" className={inputClass} type="number" min={1} max={compute === 'tenstorrent' ? 4 : 2} value={devices} onChange={(e) => setDevices(Number(e.target.value))} /></div>}
        </div>
        {compute !== 'cpu' && <div className="text-sm mt-4"><label htmlFor="job-workload">Workload</label><select id="job-workload" className={inputClass} value={mode} onChange={(e) => setMode(e.target.value as typeof mode)}><option value="command">Python script</option><option value="training-smoke">Small training check</option></select></div>}
        {mode === 'command' && <div className="text-sm my-4"><label htmlFor="job-script">Python script</label><textarea id="job-script" className={`${inputClass} font-mono min-h-32`} value={script} onChange={(e) => setScript(e.target.value)} spellCheck={false} /></div>}
        {mode === 'training-smoke' && <p className="my-4 text-sm">Trains a small regression model on each requested accelerator and checks its saved weights.</p>}
        <Button onClick={() => { void submit() }} variant={busy || (mode === 'command' && !script.trim()) || invalidDevices ? 'disabled' : 'success'} fontSize="sm">{busy ? 'Working…' : 'Submit job'}</Button>
      </Card>
      {(error || pollError || logsError) && <p role="alert" className="text-red-700">{error || pollError || logsError}</p>}
      {loading && <p>Loading jobs…</p>}
      {!loading && !error && !pollError && jobs.length === 0 && <p>No jobs yet. Submit one above.</p>}
      {selectedLogs && <Card><CardHeader header={`Logs: ${selectedLogs}`}><Button onClick={() => setSelectedLogs(null)} variant="normal" fontSize="xs">Close</Button></CardHeader><pre className="whitespace-pre-wrap break-all max-h-96 overflow-auto font-mono text-xs">{logs || 'No output yet.'}</pre></Card>}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        {jobs.map((job) => (
          <Card key={job.id}>
            <CardHeader header={job.name}>
              <Button onClick={() => { setLogs(''); setSelectedLogs(job.id) }} variant="normal" fontSize="xs">Logs</Button>
              {(job.job_state === 'Scheduled' || job.job_state === 'InProgress') && <Button onClick={() => { void cancel(job.id) }} variant={busy ? 'disabled' : 'danger'} fontSize="xs">Cancel</Button>}
            </CardHeader>
            <div className="grid grid-cols-2 gap-3 break-all">
              <CardInfoField label="Status" value={statusNames[job.job_state]} />
              <CardInfoField label="Compute" value={`${job.accelerator}${job.device_count ? ` × ${job.device_count}${job.accelerator === 'tenstorrent' ? ' boards' : ' GPUs'}` : ''}`} />
              <CardInfoField label="Job ID" value={job.id} />
              <CardInfoField label="Machine" value={job.node ?? 'Waiting for placement'} />
              <CardInfoField label="Created" value={new Date(job.created).toLocaleString()} />
              <CardInfoField label="Resources" value={`${job.cpu} CPU · ${job.memory} RAM`} />
              <CardInfoField label="Image" value={job.image} />
              <CardInfoField label="Exit code" value={job.exit_code === undefined ? '—' : String(job.exit_code)} />
            </div>
            {job.message && <p className="mt-3 text-sm break-words">{job.message}</p>}
          </Card>
        ))}
      </div>
    </div>
  )
}

import { useState } from 'react'
import Card from '#/components/Card.tsx'
import type {
  Compute,
  Dataset,
  HardwareSnapshot,
  ImageCatalog,
  Submission,
} from '#/api.ts'
import { computeNames } from '#/components/HardwarePanel.tsx'
import { errorMessage } from '#/hooks/usePolling.ts'

const fieldClass = 'w-full rounded border border-gray-300 bg-transparent p-2'
type Mode = 'script' | 'container' | 'training-smoke'

function environment(text: string) {
  const values: Record<string, string> = Object.create(null)
  for (const line of text.split('\n').filter((entry) => entry.trim())) {
    const equals = line.indexOf('=')
    if (equals < 1)
      throw new Error(
        'Environment variables must use NAME=value, one per line.',
      )
    const name = line.slice(0, equals).trim()
    if (!/^[A-Za-z_][A-Za-z0-9_]*$/.test(name))
      throw new Error(`Invalid environment variable: ${name}`)
    values[name] = line.slice(equals + 1)
  }
  return values
}

export function JobSubmissionForm({
  catalog,
  hardware,
  onSubmit,
  datasets = [],
  datasetError,
}: {
  datasets?: Dataset[]
  datasetError?: string
  catalog: ImageCatalog | null
  hardware: HardwareSnapshot | null
  onSubmit: (submission: Submission) => Promise<void>
}) {
  const [datasetID, setDatasetID] = useState('')
  const [name, setName] = useState('')
  const [compute, setCompute] = useState<Compute>('cpu')
  const [devices, setDevices] = useState(1)
  const [mode, setMode] = useState<Mode>('script')
  const [imageOverride, setImageOverride] = useState('')
  const [command, setCommand] = useState('')
  const [args, setArgs] = useState('')
  const [workingDirectory, setWorkingDirectory] = useState('')
  const [env, setEnv] = useState('')
  const [cpu, setCPU] = useState('')
  const [memory, setMemory] = useState('')
  const [timeout, setTimeoutSeconds] = useState(600)
  const [script, setScript] = useState(
    "from pathlib import Path\nPath('/outputs/hello.txt').write_text('Hello from Mist\\n')\nprint('Hello from Mist', flush=True)\n",
  )
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const profile = catalog?.profiles.find(
    (candidate) => candidate.accelerator === compute,
  )
  const image =
    (mode === 'training-smoke'
      ? profile?.default_image
      : imageOverride || profile?.default_image) || ''
  const images =
    catalog?.images.filter((option) => option.accelerators.includes(compute)) ??
    []
  const pool = hardware?.pools.find(
    (candidate) => candidate.accelerator === compute,
  )
  const approved = images.some((option) => option.reference === image)
  const invalidDevices =
    compute !== 'cpu' &&
    (!Number.isInteger(devices) ||
      devices < 1 ||
      devices > (profile?.max_devices ?? 0))
  const invalidTimeout =
    !Number.isInteger(timeout) || timeout < 10 || timeout > 86400
  const disabled =
    busy ||
    !catalog ||
    !approved ||
    invalidDevices ||
    invalidTimeout ||
    (mode === 'script' && !script.trim())

  async function submit() {
    if (disabled) return
    setBusy(true)
    setError('')
    try {
      const submission: Submission = {
        name,
        ...(datasetID ? { dataset_id: datasetID } : {}),
        type: mode === 'training-smoke' ? mode : 'command',
        accelerator: compute,
        device_count: compute === 'cpu' ? 0 : devices,
        image,
        timeout_seconds: timeout,
        ...(cpu.trim() ? { cpu: cpu.trim() } : {}),
        ...(memory.trim() ? { memory: memory.trim() } : {}),
        ...(workingDirectory.trim()
          ? { working_directory: workingDirectory.trim() }
          : {}),
        ...(env.trim() ? { env: environment(env) } : {}),
        ...(mode === 'script' ? { script, script_name: 'script.py' } : {}),
        ...(mode === 'container' && command.trim()
          ? { command: [command.trim()] }
          : {}),
        ...(mode === 'container' && args
          ? { args: args.split('\n').filter((line) => line !== '') }
          : {}),
      }
      await onSubmit(submission)
    } catch (err) {
      setError(errorMessage(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Card>
      <h2 className="text-lg font-semibold mb-4">Submit a job</h2>
      <form
        onSubmit={(event) => {
          event.preventDefault()
          void submit()
        }}
        className="space-y-4"
      >
        <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
          <div className="text-sm">
            <label htmlFor="job-name">Name</label>
            <input
              id="job-name"
              className={fieldClass}
              value={name}
              onChange={(event) => setName(event.target.value)}
              placeholder="Optional job name"
            />
          </div>
          <div className="text-sm">
            <label htmlFor="job-compute">Compute</label>
            <select
              id="job-compute"
              className={fieldClass}
              value={compute}
              onChange={(event) => {
                const next = event.target.value as Compute
                setCompute(next)
                setDevices(1)
                setImageOverride('')
                setError('')
                if (next === 'cpu' && mode === 'training-smoke')
                  setMode('script')
              }}
            >
              {(['cpu', 'nvidia', 'tenstorrent'] as const).map((value) => (
                <option key={value} value={value}>
                  {computeNames[value]}
                </option>
              ))}
            </select>
          </div>
          {compute !== 'cpu' && (
            <div className="text-sm">
              <label htmlFor="job-devices">
                {compute === 'tenstorrent' ? 'Boards (two chips each)' : 'GPUs'}
              </label>
              <input
                id="job-devices"
                className={fieldClass}
                type="number"
                min={1}
                max={profile?.max_devices}
                value={devices}
                onChange={(event) => setDevices(Number(event.target.value))}
              />
            </div>
          )}
        </div>
        {pool && (
          <p className="text-sm">
            {pool.available === null
              ? 'Availability is currently unknown.'
              : !pool.ready
                ? 'This machine is unavailable for placement; submitted jobs may wait.'
                : `${pool.available} of ${pool.total} ${pool.unit}s available now.${devices > pool.available ? ' This request will need to wait for devices.' : ''}`}
          </p>
        )}
        <div className="text-sm">
          <label htmlFor="job-workload">Workload</label>
          <select
            id="job-workload"
            className={fieldClass}
            value={mode}
            onChange={(event) => setMode(event.target.value as Mode)}
          >
            <option value="script">Python script</option>
            <option value="container">Container image</option>
            {compute !== 'cpu' && (
              <option value="training-smoke">Small training check</option>
            )}
          </select>
        </div>
        <div className="text-sm">
          <label htmlFor="job-image">Container image</label>
          <input
            id="job-image"
            className={`${fieldClass} font-mono`}
            list="approved-images"
            value={image}
            onChange={(event) => setImageOverride(event.target.value)}
            disabled={mode === 'training-smoke'}
          />
          <datalist id="approved-images">
            {images.map((option) => (
              <option key={option.reference} value={option.reference} />
            ))}
          </datalist>
          <p className="mt-1 text-xs text-gray-600">
            Choose an approved image or enter its exact registry reference. Code
            and dependencies must already be included in the image.
          </p>
          {catalog && !approved && (
            <p role="alert" className="text-red-700">
              This image is not approved for the selected hardware.
            </p>
          )}
        </div>
        <div className="text-sm">
          <label htmlFor="job-dataset">Dataset (optional)</label>
          <select
            id="job-dataset"
            className={fieldClass}
            value={datasetID}
            onChange={(e) => setDatasetID(e.target.value)}
          >
            <option value="">No dataset</option>
            {datasets.map((ds) => (
              <option key={ds.id} value={ds.id}>
                {ds.name}
              </option>
            ))}
          </select>
          <p className="mt-1 text-xs">
            Upload datasets on the Datasets page. Selected files appear
            read-only at /inputs.
          </p>
          {datasetError && (
            <p role="alert">Dataset list unavailable: {datasetError}</p>
          )}
        </div>
        {profile?.note && <p className="text-sm">{profile.note}</p>}
        {mode === 'script' && (
          <div className="text-sm">
            <label htmlFor="job-script">Python script</label>
            <textarea
              id="job-script"
              className={`${fieldClass} font-mono min-h-36`}
              value={script}
              onChange={(event) => setScript(event.target.value)}
              spellCheck={false}
            />
          </div>
        )}
        {mode === 'container' && (
          <div className="grid gap-4 md:grid-cols-2">
            <div className="text-sm">
              <label htmlFor="job-command">Command executable (optional)</label>
              <input
                id="job-command"
                className={`${fieldClass} font-mono`}
                value={command}
                onChange={(event) => setCommand(event.target.value)}
                placeholder="python"
              />
              <p className="mt-1 text-xs text-gray-600">
                Leave blank to use the image's default entrypoint. Enter the
                executable here and its arguments separately.
              </p>
            </div>
            <div className="text-sm">
              <label htmlFor="job-arguments">Arguments (one per line)</label>
              <textarea
                id="job-arguments"
                className={`${fieldClass} font-mono min-h-24`}
                value={args}
                onChange={(event) => setArgs(event.target.value)}
                placeholder={'train.py\n--epochs\n10'}
              />
            </div>
          </div>
        )}
        {mode === 'training-smoke' && (
          <p className="text-sm">
            Trains a small regression model on each requested accelerator and
            checks its saved weights.
          </p>
        )}
        <details className="text-sm">
          <summary className="cursor-pointer font-medium">
            Resources and environment
          </summary>
          <div className="grid gap-4 md:grid-cols-3 mt-3">
            <div>
              <label htmlFor="job-cpu">CPU request</label>
              <input
                id="job-cpu"
                className={fieldClass}
                value={cpu}
                onChange={(event) => setCPU(event.target.value)}
                placeholder={
                  compute === 'cpu'
                    ? '250m'
                    : compute === 'tenstorrent'
                      ? String(devices * 2)
                      : '2'
                }
              />
            </div>
            <div>
              <label htmlFor="job-memory">Memory request</label>
              <input
                id="job-memory"
                className={fieldClass}
                value={memory}
                onChange={(event) => setMemory(event.target.value)}
                placeholder={
                  compute === 'cpu'
                    ? '256Mi'
                    : compute === 'tenstorrent'
                      ? `${devices * 4}Gi`
                      : '2Gi'
                }
              />
            </div>
            <div>
              <label htmlFor="job-timeout">Deadline (seconds)</label>
              <input
                id="job-timeout"
                className={fieldClass}
                type="number"
                min={10}
                max={86400}
                value={timeout}
                onChange={(event) =>
                  setTimeoutSeconds(Number(event.target.value))
                }
              />
            </div>
          </div>
          <p className="mt-2 text-xs text-gray-600">
            Maximum 8 CPU and 32Gi RAM. The deadline includes time waiting for
            resources.
          </p>
          <div className="mt-3">
            <label htmlFor="job-directory">Working directory (optional)</label>
            <input
              id="job-directory"
              className={fieldClass}
              value={workingDirectory}
              onChange={(event) => setWorkingDirectory(event.target.value)}
              placeholder="Image default, or an absolute path such as /app"
            />
          </div>
          <div className="mt-3">
            <label htmlFor="job-environment">
              Environment variables (NAME=value, one per line)
            </label>
            <textarea
              id="job-environment"
              className={`${fieldClass} font-mono min-h-20`}
              value={env}
              onChange={(event) => setEnv(event.target.value)}
              placeholder="EPOCHS=10"
            />
          </div>
        </details>
        <p className="text-sm">
          Save results under <code>/outputs</code>. Files there remain on the
          shared storage after the job exits. Use the job’s Files button to
          download them.
        </p>
        {error && (
          <p role="alert" className="text-red-700">
            {error}
          </p>
        )}
        <button
          type="submit"
          disabled={disabled}
          className="rounded px-4 py-2 bg-green-200 text-green-800 hover:bg-green-300 disabled:bg-gray-200 disabled:text-gray-600"
        >
          {busy ? 'Submitting…' : 'Submit job'}
        </button>
      </form>
    </Card>
  )
}

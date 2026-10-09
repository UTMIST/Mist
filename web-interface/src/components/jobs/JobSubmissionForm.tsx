import { Picker } from '#/components/Picker.tsx'
import { useState } from 'react'
import Card, { PanelHeading } from '#/components/Card.tsx'
import {
  Cpu,
  CircuitBoard,
  Layers,
  Play,
  Check,
  ArrowRight,
  SlidersHorizontal,
  Code2,
} from 'lucide-react'
import type {
  Compute,
  Dataset,
  HardwareSnapshot,
  ImageCatalog,
  Submission,
} from '#/api.ts'
import { computeNames } from '#/components/HardwarePanel.tsx'
import { errorMessage } from '#/hooks/usePolling.ts'
import { useTeam } from '#/teams.tsx'
import { useAccount } from '#/auth.tsx'

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
  const { team } = useTeam()
  const { user } = useAccount()
  const [scope, setScope] = useState(user?.id ?? '')
  const [ttRuntime, setTTRuntime] = useState<'host' | 'container'>('host')
  const commonWriter =
    team?.members.find((m) => m.id === user?.id)?.common_writer ||
    user?.role.split(',').includes('admin')
  const [datasetID, setDatasetID] = useState('')
  const [name, setName] = useState('')
  const [compute, setCompute] = useState<Compute>('cpu')
  const [devices, setDevices] = useState(1)
  const [mode, setMode] = useState<Mode>('script')
  const [imageOverride, setImageOverride] = useState<string | null>(null)
  const [command, setCommand] = useState('')
  const [args, setArgs] = useState('')
  const [workingDirectory, setWorkingDirectory] = useState('')
  const [env, setEnv] = useState('')
  const [cpu, setCPU] = useState('')
  const [memory, setMemory] = useState('')
  const [timeoutOverride, setTimeoutSeconds] = useState<number | null>(null)
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
      : imageOverride ?? profile?.default_image) || ''
  const images =
    catalog?.images.filter((option) => option.accelerators.includes(compute)) ??
    []
  const pool = hardware?.pools.find(
    (candidate) => candidate.accelerator === compute,
  )
  const first = image.split('/')[0]
  const registry =
    image.includes('/') &&
    (first.includes('.') || first.includes(':') || first === 'localhost')
      ? first
      : 'docker.io'
  const approved =
    images.some((option) => option.reference === image) ||
    !!(
      catalog?.self_service &&
      catalog.registries?.includes(registry) &&
      /(:[\w.-]+|@sha256:[a-f0-9]{64})$/.test(image)
    )
  const maxDevices = Math.min(
    profile?.max_devices ?? 0,
    team
      ? compute === 'nvidia'
        ? team.policy.nvidia
        : team.policy.tenstorrent
      : Infinity,
  )
  const maxTimeout = team?.policy.runtime_seconds ?? 86400
  const timeout = timeoutOverride ?? Math.min(600, maxTimeout)
  const invalidDevices =
    compute !== 'cpu' &&
    (!Number.isInteger(devices) || devices < 1 || devices > maxDevices)
  const invalidTimeout =
    !Number.isInteger(timeout) || timeout < 10 || timeout > maxTimeout
  const disabled =
    busy ||
    !catalog ||
    (catalog.teams_enabled && !catalog.self_service) ||
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
        ...(team && scope ? { storage_scope: scope } : {}),
        ...(team && compute === 'tenstorrent' && mode !== 'training-smoke'
          ? {
              tt_runtime:
                image !== profile?.default_image
                  ? ('container' as const)
                  : ttRuntime,
            }
          : {}),
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
    <Card className="mist-submission-panel">
      <PanelHeading
        title="Submit a job"
        description="Configure an experiment and put your compute to work."
        icon={<Play size={20} />}
      />
      {catalog?.teams_enabled && !catalog.self_service && (
        <p className="mb-4">
          Choose a team workspace to submit new jobs. Historical account-owned
          jobs remain below.
        </p>
      )}
      <form
        onSubmit={(event) => {
          event.preventDefault()
          void submit()
        }}
        className="space-y-4"
      >
        <fieldset className="mist-form-section">
          <legend className="mist-section-label">
            <span>1</span> Choose compute
          </legend>
          <div
            className="mist-compute-options"
            role="radiogroup"
            aria-label="Compute"
          >
            {(['cpu', 'nvidia', 'tenstorrent'] as const).map((value) => {
              const optionPool = hardware?.pools.find(
                (p) => p.accelerator === value,
              )
              const Icon =
                value === 'cpu'
                  ? Cpu
                  : value === 'nvidia'
                    ? Layers
                    : CircuitBoard
              const allowance =
                value === 'nvidia'
                  ? team?.policy.nvidia
                  : team?.policy.tenstorrent
              const unavailable = value !== 'cpu' && allowance === 0
              return (
                <label
                  key={value}
                  className={`mist-compute-option ${compute === value ? 'is-selected' : ''} ${unavailable ? 'is-unavailable' : ''}`}
                >
                  <input
                    type="radio"
                    name="compute"
                    value={value}
                    checked={compute === value}
                    disabled={unavailable}
                    onChange={() => {
                      setCompute(value)
                      setDevices(1)
                      setImageOverride(null)
                      setError('')
                      if (value === 'cpu' && mode === 'training-smoke')
                        setMode('script')
                    }}
                  />
                  <span className="mist-choice-top">
                    <Icon size={20} />
                    <span className="mist-choice-check">
                      <Check size={12} />
                    </span>
                  </span>
                  <strong>{computeNames[value]}</strong>
                  <span>
                    {value === 'cpu'
                      ? 'General purpose'
                      : value === 'nvidia'
                        ? 'CUDA GPUs'
                        : 'TT accelerator boards'}
                  </span>
                  <small>
                    {unavailable
                      ? 'Outside team allowance'
                      : value === 'cpu'
                        ? 'On the main machine'
                        : optionPool?.available == null
                          ? 'Checking capacity…'
                          : `${optionPool.available} / ${optionPool.total} available`}
                  </small>
                </label>
              )
            })}
          </div>
          {compute !== 'cpu' && (
            <div className="mist-device-count">
              <label htmlFor="job-devices">
                {compute === 'tenstorrent' ? 'Boards (two chips each)' : 'GPUs'}
              </label>
              <input
                id="job-devices"
                className={fieldClass}
                type="number"
                min={1}
                max={maxDevices}
                value={devices}
                onChange={(event) => setDevices(Number(event.target.value))}
              />
              <input
                aria-label="Device count slider"
                type="range"
                min={1}
                max={Math.max(1, maxDevices)}
                step={1}
                value={devices}
                onChange={(e) => setDevices(Number(e.target.value))}
                className="mist-range"
              />
            </div>
          )}
        </fieldset>
        {pool && !pool.ready && (
          <p className="mist-help">This machine is offline; jobs will wait.</p>
        )}
        <fieldset className="mist-form-section">
          <legend className="mist-section-label">
            <span>2</span> Configure your workload
          </legend>
          <div
            className="mist-segmented"
            role="radiogroup"
            aria-label="Workload"
          >
            {(
              [
                ['script', 'Python'],
                ['container', 'Container'],
                ...(compute !== 'cpu'
                  ? [['training-smoke', 'Training check']]
                  : []),
              ] as [Mode, string][]
            ).map(([value, label]) => (
              <label
                key={value}
                className={mode === value ? 'is-selected' : ''}
              >
                <input
                  type="radio"
                  name="workload"
                  checked={mode === value}
                  onChange={() => setMode(value)}
                />
                {label}
              </label>
            ))}
          </div>
          <div className="text-sm">
            <label htmlFor="job-name">Name</label>
            <input
              id="job-name"
              className={fieldClass}
              value={name}
              onChange={(event) => setName(event.target.value)}
              placeholder="e.g. Baseline model · experiment 01"
            />
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
            <details className="mist-inline-help">
              <summary>Image requirements</summary>
              <p>
                Use a tag or digest from{' '}
                {catalog?.registries?.join(', ') ?? 'an approved registry'}.
                Include your code and dependencies in the image.
              </p>
            </details>
            {catalog && !approved && (
              <p role="alert" className="text-red-700">
                This image is not approved for the selected hardware.
              </p>
            )}
          </div>
          {mode === 'script' && (
            <div className="text-sm">
              <label htmlFor="job-script" className="mist-editor-label">
                <Code2 size={15} aria-hidden="true" /> Python script{' '}
                <span aria-hidden="true">script.py</span>
              </label>
              <textarea
                id="job-script"
                aria-label="Python script"
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
                <label htmlFor="job-command">
                  Command executable (optional)
                </label>
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
        </fieldset>
        <fieldset className="mist-form-section mist-data-section">
          <legend className="mist-section-label">
            <span>3</span> Data & results
          </legend>
          {team && (
            <div>
              <label htmlFor="job-output-folder">Save results in</label>
              <Picker
                id="job-output-folder"
                className={fieldClass}
                value={scope}
                onChange={(e) => setScope(e.target.value)}
              >
                <option value={user?.id}>My folder</option>
                {commonWriter && <option value="common">Common folder</option>}
              </Picker>
            </div>
          )}
          {team && compute === 'tenstorrent' && mode !== 'training-smoke' && (
            <div>
              <label htmlFor="tt-runtime">Tenstorrent runtime</label>
              <Picker
                id="tt-runtime"
                className={fieldClass}
                value={
                  image !== profile?.default_image ? 'container' : ttRuntime
                }
                disabled={image !== profile?.default_image}
                onChange={(e) =>
                  setTTRuntime(e.target.value as 'host' | 'container')
                }
              >
                <option value="host">Tested installed runtime</option>
                <option value="container">Runtime included in my image</option>
              </Picker>
              {image !== profile?.default_image && (
                <p className="mist-output-note">
                  Your image supplies TT libraries and Python.
                </p>
              )}
            </div>
          )}
          <div className="text-sm">
            <label htmlFor="job-dataset">Dataset (optional)</label>
            <Picker
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
            </Picker>
            <p className="mt-1 text-xs">Read-only input at /inputs.</p>
            {datasetError && (
              <p role="alert">Dataset list unavailable: {datasetError}</p>
            )}
          </div>
        </fieldset>

        <details className="mist-advanced text-sm">
          <summary className="cursor-pointer font-medium">
            <SlidersHorizontal size={15} aria-hidden="true" /> Resources and
            environment
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
                max={maxTimeout}
                value={timeout}
                onChange={(event) =>
                  setTimeoutSeconds(Number(event.target.value))
                }
              />
            </div>
          </div>
          <p className="mt-2 text-xs text-gray-600">
            Maximum 8 CPU and 32Gi RAM per job.{' '}
            {team
              ? 'The deadline starts after admission; jobs can wait in the queue for up to 24 hours.'
              : 'The deadline includes time waiting for resources.'}
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
        <p className="mist-output-note">
          Results saved under <code>/outputs</code> stay in your team folder.
        </p>
        {invalidDevices && (
          <p role="alert" className="text-red-700">
            Choose between 1 and {maxDevices}{' '}
            {compute === 'nvidia' ? 'GPUs' : 'boards'} within your team
            allowance.
          </p>
        )}
        {invalidTimeout && (
          <p role="alert" className="text-red-700">
            The deadline must be between 10 and {maxTimeout} seconds.
          </p>
        )}
        {error && (
          <p role="alert" className="text-red-700">
            {error}
          </p>
        )}
        <button
          type="submit"
          disabled={disabled}
          className="mist-button mist-button-primary mist-submit-button"
        >
          <Play size={15} aria-hidden="true" />{' '}
          {busy ? 'Submitting…' : 'Submit job'}{' '}
          <ArrowRight size={16} aria-hidden="true" />
        </button>
      </form>
    </Card>
  )
}

import { Picker } from '#/components/Picker.tsx'
import { Modal } from '#/components/Modal.tsx'
import { formatBytes } from '#/format.ts'
import { useState, useRef, useEffect } from 'react'
import {
  UploadCloud,
  Database,
  Trash2,
  FileCheck2,
  LoaderCircle,
} from 'lucide-react'
import { PanelHeading } from '#/components/Card.tsx'
import { storageAPI } from '#/api.ts'
import { errorMessage, usePolling } from '#/hooks/usePolling.ts'
import { useTeam } from '#/teams.tsx'
import { useAccount } from '#/auth.tsx'
import { TeamStorageBrowser } from '#/components/TeamStorageBrowser.tsx'

export function DatasetsPage() {
  const { selected, team } = useTeam()
  const { user } = useAccount()
  const [scope, setScope] = useState(user?.id ?? '')
  const commonWriter =
    team?.members.find((m) => m.id === user?.id)?.common_writer ||
    user?.role.split(',').includes('admin')
  const datasets = usePolling(storageAPI.list, 10000),
    storage = usePolling(storageAPI.info, 15000)
  const [file, setFile] = useState<File | null>(null),
    [name, setName] = useState(''),
    [zip, setZip] = useState(false),
    [progress, setProgress] = useState(0),
    [busy, setBusy] = useState(false),
    [error, setError] = useState(''),
    [message, setMessage] = useState('')
  const [uploading, setUploading] = useState(false)
  const uploadRequest = useRef<AbortController | null>(null)
  useEffect(() => () => uploadRequest.current?.abort(), [])
  const [search, setSearch] = useState('')
  const [dragging, setDragging] = useState(false)
  function chooseFile(next: File | null) {
    setFile(next)
    setZip(next?.name.toLowerCase().endsWith('.zip') ?? false)
  }
  const limit = datasets.data?.upload_limit_bytes ?? 0
  const isPreparing = (value: string) =>
    value.includes('team storage is being provisioned') ||
    value.includes('team storage is not ready')
  const storagePending =
    !!selected &&
    (datasets.loading ||
      storage.loading ||
      isPreparing(datasets.error) ||
      isPreparing(storage.error))
  const visibleDatasets =
    datasets.data?.datasets.filter((ds) =>
      ds.name.toLowerCase().includes(search.toLowerCase()),
    ) ?? []
  async function upload() {
    if (!file) return
    setBusy(true)
    setError('')
    setMessage('')
    setProgress(0)
    try {
      if (file.size > limit) throw new Error('Dataset exceeds the upload limit')
      uploadRequest.current = new AbortController()
      const ds = await storageAPI.upload(
        file,
        name,
        zip,
        setProgress,
        scope,
        uploadRequest.current.signal,
      )
      setMessage(`${ds.name} is ready to use.`)
      setUploading(false)
      datasets.refresh()
      storage.refresh()
    } catch (err) {
      setError(errorMessage(err))
    } finally {
      uploadRequest.current = null
      setBusy(false)
    }
  }
  async function remove(id: string) {
    setBusy(true)
    setError('')
    try {
      await storageAPI.remove(id)
      datasets.refresh()
      storage.refresh()
    } catch (err) {
      setError(errorMessage(err))
    } finally {
      setBusy(false)
    }
  }
  return (
    <main className="px-6 py-8 lg:px-16 space-y-6">
      <div className="mist-page-heading">
        <div>
          <p className="mist-eyebrow">{team?.name ?? 'Legacy workspace'}</p>
          <h1 className="text-2xl font-bold">Datasets</h1>
          <p className="mist-description">
            A shared home for your team's data and experiment results.
          </p>
        </div>
        <button
          type="button"
          className="mist-button mist-button-primary"
          disabled={!selected}
          onClick={() => setUploading(true)}
        >
          <UploadCloud size={16} aria-hidden="true" /> Add dataset
        </button>
      </div>
      {storagePending && (
        <p role="status" className="mist-storage-pending">
          <LoaderCircle
            size={15}
            className="motion-safe:animate-spin"
            aria-hidden="true"
          />
          Getting team storage ready. You can choose your file while it
          finishes.
        </p>
      )}
      {storage.data?.enabled && (
        <div className="mist-storage-meter">
          <div>
            <Database size={17} aria-hidden="true" />
            <span>Team storage</span>
            <strong>
              {formatBytes(
                storage.data.capacity_bytes - storage.data.available_bytes,
              )}{' '}
              / {formatBytes(storage.data.capacity_bytes)}
            </strong>
          </div>
          <progress
            aria-label="Storage usage"
            value={storage.data.capacity_bytes - storage.data.available_bytes}
            max={storage.data.capacity_bytes}
          />
          <small>{formatBytes(storage.data.available_bytes)} available</small>
        </div>
      )}
      {uploading && (
        <Modal
          title="Upload dataset"
          closeDisabled={busy}
          onClose={() => {
            if (!busy) setUploading(false)
          }}
        >
          <form
            className="mist-upload-panel mist-card border rounded p-6 space-y-4"
            onSubmit={(e) => {
              e.preventDefault()
              void upload()
            }}
          >
            <PanelHeading
              title="Upload a dataset"
              description="Add a file or ZIP, then attach it to any experiment."
              icon={<UploadCloud size={20} />}
            />
            {selected && (
              <div>
                <label htmlFor="dataset-scope">Upload folder</label>
                <Picker
                  id="dataset-scope"
                  className="w-full border rounded p-2"
                  value={scope}
                  disabled={busy}
                  onChange={(e) => setScope(e.target.value)}
                >
                  <option value={user?.id}>My folder</option>
                  {commonWriter && (
                    <option value="common">Common folder</option>
                  )}
                </Picker>
                <p className="text-sm">
                  All teammates can read these files. Other teams need an
                  explicit sharing grant.
                </p>
              </div>
            )}
            <div>
              <label htmlFor="dataset-name">Dataset name (optional)</label>
              <input
                id="dataset-name"
                className="w-full border rounded p-2"
                maxLength={120}
                value={name}
                disabled={busy}
                onChange={(e) => setName(e.target.value)}
              />
            </div>
            <div
              className={`mist-upload-zone ${dragging ? 'is-dragging' : ''} ${file ? 'has-file' : ''}`}
              onDragOver={(e) => {
                e.preventDefault()
                if (!busy) setDragging(true)
              }}
              onDragLeave={() => setDragging(false)}
              onDrop={(e) => {
                e.preventDefault()
                setDragging(false)
                if (!busy) chooseFile(e.dataTransfer.files.item(0))
              }}
            >
              {file ? (
                <FileCheck2 size={28} aria-hidden="true" />
              ) : (
                <UploadCloud size={28} aria-hidden="true" />
              )}
              <label htmlFor="dataset-file">File or ZIP archive</label>
              <strong>
                {file ? file.name : 'Click to choose a file, or drop it here'}
              </strong>
              <span>
                {file
                  ? `${formatBytes(file.size)} · Click to replace`
                  : storagePending
                    ? 'Getting storage ready…'
                    : `Up to ${formatBytes(limit)} in this workspace`}
              </span>
              <input
                id="dataset-file"
                type="file"
                disabled={busy}
                onChange={(e) => chooseFile(e.target.files?.[0] ?? null)}
              />
            </div>
            <label className="flex gap-2">
              <input
                type="checkbox"
                checked={zip}
                disabled={busy}
                onChange={(e) => setZip(e.target.checked)}
              />
              Extract ZIP into dataset files
            </label>
            <p className="mist-output-note">
              {storagePending
                ? 'Upload will be available when team storage is ready.'
                : `Upload limit: ${formatBytes(limit)}. ZIP extraction also needs space for the expanded files.`}
            </p>
            <button
              disabled={busy || !file || !limit || !selected}
              className="mist-button mist-button-primary"
            >
              <UploadCloud size={16} aria-hidden="true" />{' '}
              {busy ? 'Uploading…' : 'Upload dataset'}
            </button>
            {error && <p role="alert">{error}</p>}
            {busy && (
              <button
                type="button"
                className="mist-button"
                onClick={() => uploadRequest.current?.abort()}
              >
                Cancel upload
              </button>
            )}
            {busy && (
              <div role="status">
                <progress
                  max={100}
                  value={progress}
                  aria-label="Dataset upload progress"
                />
                <p>
                  {progress === 100
                    ? 'Validating and saving dataset…'
                    : `${progress}% uploaded`}
                </p>
              </div>
            )}
          </form>
        </Modal>
      )}
      {(error ||
        (!isPreparing(datasets.error) && datasets.error) ||
        (!isPreparing(storage.error) && storage.error)) && (
        <p role="alert" className="text-red-700">
          {error ||
            (!isPreparing(datasets.error) && datasets.error) ||
            storage.error}
        </p>
      )}
      {message && <p role="status">{message}</p>}
      <section className="mist-card mist-dataset-list border p-5">
        <PanelHeading
          title="Dataset library"
          description="Ready inputs for your team’s experiments."
          icon={<Database size={20} />}
          action={
            <span className="mist-badge">
              {datasets.data?.datasets.length ?? 0} datasets
            </span>
          }
        />
        <div className="mist-library-toolbar">
          <input
            aria-label="Search datasets"
            placeholder="Find a dataset…"
            value={search}
            onChange={(event) => setSearch(event.target.value)}
          />
        </div>
        <ul className="space-y-3">
          {visibleDatasets.map((ds) => (
            <li
              key={ds.id}
              className="border rounded p-4 flex flex-wrap justify-between gap-4"
            >
              <div>
                <h2 className="font-semibold">{ds.name}</h2>
                {ds.team_id && (
                  <p className="text-sm">
                    {ds.team_name ?? team?.name} /{' '}
                    {ds.scope === 'common' ? 'Common' : ds.member_name}
                    {ds.shared ? ' · Shared read-only' : ''}
                  </p>
                )}
                <p>
                  {ds.files} file(s) · {formatBytes(ds.size)} · {ds.state}
                </p>
                <details className="mist-inline-help mist-dataset-metadata">
                  <summary>File details</summary>
                  <code>{ds.id}</code>
                  <p>SHA256: {ds.sha256}</p>
                </details>
              </div>
              <button
                disabled={
                  busy ||
                  ds.shared ||
                  (!!ds.scope &&
                    ds.scope !== user?.id &&
                    !(ds.scope === 'common' && commonWriter))
                }
                className="mist-button mist-button-danger"
                onClick={() => void remove(ds.id)}
              >
                <Trash2 size={14} aria-hidden="true" /> Delete dataset
              </button>
            </li>
          ))}
        </ul>
        {!datasets.loading &&
          !datasets.error &&
          visibleDatasets.length === 0 && (
            <p className="mist-empty-state">
              {search
                ? 'No datasets match your search.'
                : 'No datasets yet. Add your first dataset to get started.'}
            </p>
          )}
      </section>
      {selected ? (
        <details className="mist-folder-details">
          <summary>
            Team folders <span>Browse shared files & results</span>
          </summary>
          <TeamStorageBrowser />
        </details>
      ) : (
        <p className="mist-help">
          Choose a team to upload. These are your preserved account files.
        </p>
      )}
    </main>
  )
}

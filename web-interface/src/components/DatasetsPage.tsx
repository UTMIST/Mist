import { useState } from 'react'
import { storageAPI } from '#/api.ts'
import { errorMessage, usePolling } from '#/hooks/usePolling.ts'

export function DatasetsPage() {
  const datasets = usePolling(storageAPI.list, 10000),
    storage = usePolling(storageAPI.info, 15000)
  const [file, setFile] = useState<File | null>(null),
    [name, setName] = useState(''),
    [zip, setZip] = useState(false),
    [progress, setProgress] = useState(0),
    [busy, setBusy] = useState(false),
    [error, setError] = useState(''),
    [message, setMessage] = useState('')
  const limit = datasets.data?.upload_limit_bytes ?? 0
  async function upload() {
    if (!file) return
    setBusy(true)
    setError('')
    setMessage('')
    setProgress(0)
    try {
      if (file.size > limit) throw new Error('Dataset exceeds the upload limit')
      const ds = await storageAPI.upload(file, name, zip, setProgress)
      setMessage(`${ds.name} is ready to use.`)
      datasets.refresh()
      storage.refresh()
    } catch (err) {
      setError(errorMessage(err))
    } finally {
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
      <h1 className="text-2xl font-bold">Datasets</h1>
      <p>
        Datasets are stored on QuietBox and can be read by jobs on either
        machine. Each job mounts its selected dataset read-only at{' '}
        <code>/inputs</code>.
      </p>
      {storage.data?.enabled && (
        <p>
          {(storage.data.available_bytes / 2 ** 30).toFixed(1)} GiB free of{' '}
          {(storage.data.capacity_bytes / 2 ** 30).toFixed(1)} GiB shared
          storage.
        </p>
      )}
      <form
        className="border rounded p-6 space-y-4 max-w-xl"
        onSubmit={(e) => {
          e.preventDefault()
          void upload()
        }}
      >
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
        <div>
          <label htmlFor="dataset-file">File or ZIP archive</label>
          <input
            id="dataset-file"
            type="file"
            required
            disabled={busy}
            className="block mt-2"
            onChange={(e) => {
              setFile(e.target.files?.[0] ?? null)
              setZip(
                e.target.files?.[0]?.name.toLowerCase().endsWith('.zip') ??
                  false,
              )
            }}
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
        <p className="text-sm">
          Maximum 2 GiB uploaded or extracted. ZIP paths and special files are
          checked before publication.
        </p>
        <button
          disabled={busy || !file || !limit}
          className="bg-green-200 p-2 rounded"
        >
          {busy ? 'Uploading…' : 'Upload dataset'}
        </button>
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
      {(error || datasets.error || storage.error) && (
        <p role="alert" className="text-red-700">
          {error || datasets.error || storage.error}
        </p>
      )}
      {message && <p role="status">{message}</p>}
      <ul className="space-y-3">
        {datasets.data?.datasets.map((ds) => (
          <li
            key={ds.id}
            className="border rounded p-4 flex flex-wrap justify-between gap-4"
          >
            <div>
              <h2 className="font-semibold">{ds.name}</h2>
              <p>
                {ds.files} file(s) · {(ds.size / 2 ** 20).toFixed(2)} MiB ·{' '}
                {ds.state}
              </p>
              <code className="text-xs">{ds.id}</code>
              <p className="text-xs break-all">Upload SHA256: {ds.sha256}</p>
            </div>
            <button disabled={busy} onClick={() => void remove(ds.id)}>
              Delete dataset
            </button>
          </li>
        ))}
      </ul>
      {!datasets.loading &&
        !datasets.error &&
        datasets.data?.datasets.length === 0 && <p>No datasets yet.</p>}
    </main>
  )
}

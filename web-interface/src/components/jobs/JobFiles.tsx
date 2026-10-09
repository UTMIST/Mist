import { useCallback } from 'react'
import { storageAPI } from '#/api.ts'
import { usePolling } from '#/hooks/usePolling.ts'

export function JobFiles({ id }: { id: string }) {
  const load = useCallback(
    (signal?: AbortSignal) => storageAPI.files(id, signal),
    [id],
  )
  const files = usePolling(load, 10000)
  return (
    <section className="mt-4 border-t pt-3">
      <h3 className="font-semibold">Saved files</h3>
      {files.loading && <p>Loading files…</p>}
      {files.error && <p role="alert">{files.error}</p>}
      <ul>
        {files.data?.files.map((file) => (
          <li key={file.path} className="flex gap-4 justify-between">
            <a
              className="underline break-all"
              href={storageAPI.downloadURL(id, file.path)}
            >
              {file.path}
            </a>
            <span className="text-sm shrink-0">
              {(file.size / 1024).toFixed(1)} KiB
            </span>
          </li>
        ))}
      </ul>
      {!files.loading && !files.error && files.data?.files.length === 0 && (
        <p>
          No saved files yet. Save job outputs under <code>/outputs</code>.
        </p>
      )}
    </section>
  )
}

import { formatBytes } from '#/format.ts'
import { Picker } from '#/components/Picker.tsx'
import { useCallback, useState } from 'react'
import { storageAPI } from '#/api.ts'
import type { StorageFolder } from '#/api.ts'
import { usePolling } from '#/hooks/usePolling.ts'
import { FolderOpen, Folder, ArrowDownToLine, Users } from 'lucide-react'
import { PanelHeading } from '#/components/Card.tsx'

function FolderFiles({ folder }: { folder: StorageFolder }) {
  const load = useCallback(
    (signal: AbortSignal) => storageAPI.folderFiles(folder, signal),
    [folder.team_id, folder.scope],
  )
  const files = usePolling(load, 10000)
  return (
    <section className="border rounded p-4 space-y-3">
      <h3 className="font-semibold">
        {folder.team_name} / {folder.name}
        {folder.shared ? ' · Shared read-only' : ''}
      </h3>
      {files.loading && <p>Loading files…</p>}
      {files.error && <p role="alert">{files.error}</p>}
      {files.data?.files.length === 0 && <p>No files in this folder yet.</p>}
      <ul className="max-h-96 overflow-auto">
        {files.data?.files.map((file) => (
          <li
            key={file.path}
            className="py-2 border-t flex justify-between gap-3"
          >
            <span className="break-all">
              {file.path} · {formatBytes(file.size)}
            </span>
            <a
              className="mist-button mist-button-normal shrink-0"
              href={storageAPI.folderDownloadURL(folder, file.path)}
            >
              <ArrowDownToLine size={14} aria-hidden="true" /> Download
            </a>
          </li>
        ))}
      </ul>
    </section>
  )
}
export function TeamStorageBrowser() {
  const folders = usePolling(storageAPI.folders, 15000)
  const [selected, setSelected] = useState('')
  const folder = folders.data?.folders.find(
    (f) => `${f.team_id}:${f.scope}` === selected,
  )
  return (
    <section className="mist-storage-panel mist-card border p-5 space-y-3">
      <PanelHeading
        title="Team folders"
        description="Browse shared data and your teammates’ saved results."
        icon={<FolderOpen size={20} />}
      />
      <div className="mist-folder-grid">
        {folders.data?.folders.map((f) => {
          const id = `${f.team_id}:${f.scope}`
          return (
            <button
              type="button"
              key={id}
              aria-pressed={selected === id}
              className={`mist-folder-tile ${selected === id ? 'is-selected' : ''}`}
              onClick={() => setSelected(selected === id ? '' : id)}
            >
              {f.scope === 'common' ? (
                <Users size={21} aria-hidden="true" />
              ) : (
                <Folder size={21} aria-hidden="true" />
              )}
              <strong>{f.name}</strong>
              <span>{f.team_name}</span>
              <small>
                {f.shared
                  ? 'Shared · read only'
                  : f.scope === 'common'
                    ? 'Team space'
                    : 'Member space'}
              </small>
            </button>
          )
        })}
      </div>
      <label htmlFor="storage-folder">Browse common or teammate files</label>
      <Picker
        id="storage-folder"
        className="border rounded p-2 block"
        value={selected}
        onChange={(e) => setSelected(e.target.value)}
      >
        <option value="">Select folder</option>
        {folders.data?.folders.map((f) => (
          <option
            key={`${f.team_id}:${f.scope}`}
            value={`${f.team_id}:${f.scope}`}
          >
            {f.team_name} / {f.name}
            {f.shared ? ' (shared read-only)' : ''}
          </option>
        ))}
      </Picker>
      {folders.error && <p role="alert">{folders.error}</p>}
      {folder && <FolderFiles key={selected} folder={folder} />}
    </section>
  )
}

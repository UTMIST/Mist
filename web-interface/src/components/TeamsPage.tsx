import { Picker } from '#/components/Picker.tsx'
import { useState, useEffect } from 'react'
import { Modal } from '#/components/Modal.tsx'
import { Tabs } from '#/components/Tabs.tsx'
import {
  Users,
  Settings2,
  ArrowUpRight,
  Plus,
  UserMinus,
  Layers,
  ShieldCheck,
  Check,
} from 'lucide-react'
import { PanelHeading } from '#/components/Card.tsx'
import type { Workspace, TeamPolicy, WorkspaceType } from '#/api.ts'
import { teamsAPI } from '#/api.ts'
import { authClient, useAccount, isUserAdmin } from '#/auth.tsx'
import { useTeam } from '#/teams.tsx'
import { errorMessage, usePolling } from '#/hooks/usePolling.ts'

const field = 'border rounded p-2 w-full text-sm'

async function loadAccounts() {
  const result = await authClient.admin.listUsers({ query: { limit: 1000 } })
  if (result.error) throw new Error(result.error.message)
  return result.data.users
}

export function workspaceTypeBadge(type?: WorkspaceType) {
  switch (type) {
    case 'personal':
      return {
        label: 'Personal Quota',
        className: 'bg-purple-100 text-purple-800 border-purple-200',
        icon: '👤',
      }
    case 'collaboration':
      return {
        label: 'Collaboration',
        className: 'bg-amber-100 text-amber-800 border-amber-200',
        icon: '⚡',
      }
    case 'admin':
      return {
        label: 'Infra Admin',
        className: 'bg-rose-100 text-rose-800 border-rose-200',
        icon: '🛡️',
      }
    case 'team':
    default:
      return {
        label: 'Team Workspace',
        className: 'bg-blue-100 text-blue-800 border-blue-200',
        icon: '👥',
      }
  }
}

function PolicyForm({
  team,
  save,
}: {
  team: Workspace
  save: (policy: TeamPolicy) => Promise<void>
}) {
  const [policy, setPolicyState] = useState(team.policy)
  const [editing, setEditing] = useState(false)
  const signature = JSON.stringify(team.policy)
  useEffect(() => {
    if (!editing) setPolicyState(JSON.parse(signature) as TeamPolicy)
  }, [signature, editing])
  function setPolicy(next: TeamPolicy) {
    setEditing(true)
    setPolicyState(next)
  }
  const [busy, setBusy] = useState(false)
  const [message, setMessage] = useState('')
  const numbers = [
    ['nvidia', 'NVIDIA GPUs', 0, 2],
    ['tenstorrent', 'Tenstorrent boards (two chips each)', 0, 4],
    ['concurrent', 'Simultaneous jobs', 1, 16],
    ['queued', 'Queued jobs', 1, 200],
    ['storage_gib', 'Dataset allowance (GiB, advisory)', 1, 1048576],
    ['model_storage_gib', 'Model allowance (GiB, advisory)', 1, 1048576],
    ['runtime_seconds', 'Maximum job runtime (seconds)', 10, 86400],
  ] as const
  return (
    <form
      className="space-y-4 border-t pt-4"
      onSubmit={async (e) => {
        e.preventDefault()
        setBusy(true)
        setMessage('')
        try {
          await save(policy)
          setEditing(false)
          setMessage('Workspace limits saved.')
        } catch (err) {
          setMessage(errorMessage(err))
        } finally {
          setBusy(false)
        }
      }}
    >
      <h3 className="font-semibold text-sm">Resource Quota & Limits</h3>
      <div className="grid md:grid-cols-3 gap-3">
        <div>
          <label htmlFor={`${team.id}-cpu`} className="text-xs font-medium text-gray-700">Total CPU cores</label>
          <input
            id={`${team.id}-cpu`}
            className={field}
            required
            value={policy.cpu}
            onChange={(e) => setPolicy({ ...policy, cpu: e.target.value })}
          />
        </div>
        <div>
          <label htmlFor={`${team.id}-memory`} className="text-xs font-medium text-gray-700">
            Total memory (e.g. 16Gi)
          </label>
          <input
            id={`${team.id}-memory`}
            className={field}
            required
            value={policy.memory}
            onChange={(e) => setPolicy({ ...policy, memory: e.target.value })}
          />
        </div>
        {numbers.map(([key, name, min, max]) => (
          <div key={key}>
            <label htmlFor={`${team.id}-${key}`} className="text-xs font-medium text-gray-700">{name}</label>
            <input
              id={`${team.id}-${key}`}
              className={field}
              type="number"
              required
              min={min}
              max={max}
              value={policy[key]}
              onChange={(e) =>
                setPolicy({ ...policy, [key]: Number(e.target.value) })
              }
            />
            {!key.includes('storage') && (
              <input
                type="range"
                aria-label={`${name} slider`}
                min={min}
                max={max}
                step={1}
                value={policy[key]}
                onChange={(e) =>
                  setPolicy({ ...policy, [key]: Number(e.target.value) })
                }
                className="mist-range w-full mt-2"
              />
            )}
          </div>
        ))}
      </div>
      <div>
        <label htmlFor={`${team.id}-registries`} className="text-xs font-medium text-gray-700">
          Allowed container registries (one per line)
        </label>
        <textarea
          id={`${team.id}-registries`}
          className={field}
          rows={3}
          value={policy.registries.join('\n')}
          onChange={(e) =>
            setPolicy({
              ...policy,
              registries: e.target.value
                .split('\n')
                .map((v) => v.trim())
                .filter(Boolean),
            })
          }
        />
      </div>
      <p className="text-xs text-gray-500">
        Compute limits apply to workloads in this workspace. Storage allowances are advisory:
        workspaces share the disk, and changing an allowance does not reserve or remove space.
      </p>
      <button disabled={busy} className="mist-button mist-button-primary text-xs">
        Save limits
      </button>
      {message && <p role="status" className="text-xs text-green-700">{message}</p>}
    </form>
  )
}

function WorkspaceAdministration({
  team,
  teams,
  refresh,
}: {
  team: Workspace
  teams: Workspace[]
  refresh: () => void
}) {
  const accounts = usePolling(loadAccounts, 30000)
  const [userID, setUserID] = useState('')
  const [commonWriter, setCommonWriter] = useState(false)
  const [target, setTarget] = useState('')
  const [scope, setScope] = useState('common')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [tab, setTab] = useState('members')

  const [name, setName] = useState(team.name)
  const [type, setType] = useState<WorkspaceType>(team.type ?? 'team')
  const [description, setDescription] = useState(team.description ?? '')
  const [generalMessage, setGeneralMessage] = useState('')

  async function action(run: () => Promise<unknown>) {
    setBusy(true)
    setError('')
    try {
      await run()
      refresh()
    } catch (err) {
      setError(errorMessage(err))
    } finally {
      setBusy(false)
    }
  }

  const badge = workspaceTypeBadge(team.type)

  return (
    <section className="border rounded p-6 space-y-5">
      <div className="flex items-start justify-between gap-4">
        <div>
          <div className="flex items-center gap-2 mb-1">
            <span className={`text-xs px-2 py-0.5 rounded border font-medium ${badge.className}`}>
              {badge.icon} {badge.label}
            </span>
            <span className="text-xs text-gray-500 font-mono">{team.id}</span>
          </div>
          <PanelHeading
            title={team.name}
            description={`${team.members.length} members · ${team.policy.storage_gib} GiB datasets · ${team.policy.model_storage_gib} GiB models${team.disabled ? ' · Disabled' : ''}`}
            icon={<Layers size={20} />}
          />
          {team.description && (
            <p className="text-xs text-gray-600 mt-1">{team.description}</p>
          )}
        </div>
        <button
          disabled={busy}
          className={`text-xs px-3 py-1.5 rounded font-medium border ${
            team.disabled
              ? 'bg-green-50 text-green-700 border-green-200'
              : 'bg-rose-50 text-rose-700 border-rose-200'
          }`}
          onClick={() =>
            void action(() =>
              teamsAPI.update(team.id, { disabled: !team.disabled }),
            )
          }
        >
          {team.disabled ? 'Enable workspace' : 'Disable workspace'}
        </button>
      </div>

      <Tabs
        id="team-settings"
        label="Workspace settings"
        value={tab}
        onChange={setTab}
        items={[
          ['members', 'Members'],
          ['general', 'General'],
          ['limits', 'Quota & Limits'],
          ['sharing', 'Data Sharing'],
        ]}
      />

      {/* General Settings */}
      <div
        role="tabpanel"
        id="team-settings-general-panel"
        aria-labelledby="team-settings-general-tab"
        hidden={tab !== 'general'}
        className="space-y-4"
      >
        <form
          className="space-y-3"
          onSubmit={(e) => {
            e.preventDefault()
            setGeneralMessage('')
            void action(async () => {
              await teamsAPI.update(team.id, { name, type, description })
              setGeneralMessage('Workspace details updated.')
            })
          }}
        >
          <div>
            <label htmlFor="ws-edit-name" className="text-xs font-medium text-gray-700">Workspace Name</label>
            <input
              id="ws-edit-name"
              className={field}
              required
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
          </div>
          <div>
            <label htmlFor="ws-edit-type" className="text-xs font-medium text-gray-700">Workspace Type</label>
            <Picker
              id="ws-edit-type"
              className={field}
              value={type}
              onChange={(e) => setType(e.target.value as WorkspaceType)}
            >
              <option value="team">Team Workspace</option>
              <option value="personal">Personal Quota</option>
              <option value="collaboration">Team Collaboration</option>
              <option value="admin">Infra Admin Allocation</option>
            </Picker>
          </div>
          <div>
            <label htmlFor="ws-edit-desc" className="text-xs font-medium text-gray-700">Description</label>
            <textarea
              id="ws-edit-desc"
              className={field}
              rows={2}
              value={description}
              onChange={(e) => setDescription(e.target.value)}
            />
          </div>
          <button disabled={busy} className="mist-button mist-button-primary text-xs">
            Save Details
          </button>
          {generalMessage && <p className="text-xs text-green-700">{generalMessage}</p>}
        </form>
      </div>

      {/* Members */}
      <div
        role="tabpanel"
        id="team-settings-members-panel"
        aria-labelledby="team-settings-members-tab"
        hidden={tab !== 'members'}
      >
        <form
          className="flex flex-wrap justify-between gap-3 items-end mb-5 bg-gray-50 p-3 rounded border"
          onSubmit={(e) => {
            e.preventDefault()
            void action(() => teamsAPI.enroll(team.id, userID, commonWriter))
          }}
        >
          <div className="basis-full min-w-0">
            <label htmlFor={`${team.id}-member`} className="text-xs font-medium text-gray-700">Existing Mist Account</label>
            <Picker
              id={`${team.id}-member`}
              className={field}
              required
              value={userID}
              onChange={(e) => setUserID(e.target.value)}
            >
              <option value="">Select account to enroll...</option>
              {accounts.data?.map((m) => (
                <option key={m.id} value={m.id}>
                  {m.name} ({m.email})
                </option>
              ))}
            </Picker>
          </div>
          <label className="flex items-center gap-2 text-xs text-gray-700">
            <input
              type="checkbox"
              checked={commonWriter}
              onChange={(e) => setCommonWriter(e.target.checked)}
            />
            Can write common workspace folder
          </label>
          <button
            disabled={busy || !userID}
            className="mist-button mist-button-primary text-xs"
          >
            Add / Update Member
          </button>
        </form>
        {accounts.error && (
          <p role="alert" className="text-xs text-red-600 mb-2">Accounts unavailable: {accounts.error}</p>
        )}
        <ul className="mist-member-list">
          {team.members.map((m) => (
            <li key={m.id}>
              <div className="mist-member-identity">
                <strong>{m.name}</strong>
                <span>{m.email}</span>
              </div>
              <span className="mist-badge">
                {m.common_writer ? 'Common writer' : 'Member'}
              </span>
              <button
                aria-label={`Remove ${m.name} from workspace`}
                title="Remove member"
                disabled={busy}
                onClick={() =>
                  void action(() => teamsAPI.removeMember(team.id, m.id))
                }
              >
                <UserMinus size={15} aria-hidden="true" />
              </button>
            </li>
          ))}
          {team.members.length === 0 && (
            <li className="text-xs text-gray-500 py-3 text-center">
              No members enrolled yet. Add an account above.
            </li>
          )}
        </ul>
      </div>

      {/* Limits */}
      <div
        role="tabpanel"
        id="team-settings-limits-panel"
        aria-labelledby="team-settings-limits-tab"
        hidden={tab !== 'limits'}
      >
        <PolicyForm
          team={team}
          save={async (policy) => {
            await teamsAPI.update(team.id, { policy })
            refresh()
          }}
        />
      </div>

      {/* Sharing Grants */}
      <section
        role="tabpanel"
        id="team-settings-sharing-panel"
        aria-labelledby="team-settings-sharing-tab"
        hidden={tab !== 'sharing'}
        className="border-t pt-4 space-y-3"
      >
        <h3 className="font-semibold text-sm">Share Datasets with Another Workspace</h3>
        <p className="text-xs text-gray-600">
          Recipients can browse and download files, and mount datasets as read-only job inputs
          at /inputs. They cannot alter this workspace's files.
        </p>
        <form
          className="grid md:grid-cols-3 gap-3 bg-gray-50 p-3 rounded border"
          onSubmit={(e) => {
            e.preventDefault()
            void action(() => teamsAPI.grant(team.id, target, scope))
          }}
        >
          <div>
            <label htmlFor={`${team.id}-scope`} className="text-xs font-medium text-gray-700">Source Folder</label>
            <Picker
              id={`${team.id}-scope`}
              className={field}
              value={scope}
              onChange={(e) => setScope(e.target.value)}
            >
              <option value="common">Common Folder</option>
              {team.members.map((m) => (
                <option key={m.id} value={m.id}>
                  {m.name}'s Folder
                </option>
              ))}
            </Picker>
          </div>
          <div>
            <label htmlFor={`${team.id}-target`} className="text-xs font-medium text-gray-700">Recipient Workspace</label>
            <Picker
              id={`${team.id}-target`}
              className={field}
              required
              value={target}
              onChange={(e) => setTarget(e.target.value)}
            >
              <option value="">Select recipient...</option>
              {teams
                .filter((t) => t.id !== team.id && !t.disabled)
                .map((t) => (
                  <option key={t.id} value={t.id}>
                    {t.name} ({t.type ?? 'team'})
                  </option>
                ))}
            </Picker>
          </div>
          <button
            disabled={busy || !target}
            className="mist-button mist-button-primary text-xs self-end"
          >
            Grant Read/Use Access
          </button>
        </form>
        <ul className="space-y-2">
          {team.grants.map((g) => (
            <li key={g.id} className="flex justify-between items-center text-xs border rounded p-2.5 bg-white">
              <span>
                <strong>
                  {g.scope === 'common'
                    ? 'Common Folder'
                    : (team.members.find((m) => m.id === g.scope)?.name ?? 'Former member')}
                </strong>{' '}
                → {teams.find((t) => t.id === g.target_team)?.name ?? g.target_team}
              </span>
              <button
                disabled={busy}
                className="text-red-700 hover:text-red-900 font-medium"
                onClick={() =>
                  void action(() => teamsAPI.revoke(team.id, g.id))
                }
              >
                Revoke access
              </button>
            </li>
          ))}
          {team.grants.length === 0 && (
            <p className="text-xs text-gray-500 py-2">
              No active dataset grants. Files in this workspace are private to its members.
            </p>
          )}
        </ul>
      </section>

      {error && (
        <p role="alert" className="text-xs text-red-700">
          {error}
        </p>
      )}
    </section>
  )
}

export function TeamsPage() {
  const { user } = useAccount()
  const { teams, refresh, choose, storageBudget, selected } = useTeam()
  const admin = isUserAdmin(user)
  const [name, setName] = useState('')
  const [wsType, setWsType] = useState<WorkspaceType>('team')
  const [description, setDescription] = useState('')
  const [assignedUser, setAssignedUser] = useState('')
  const [filterType, setFilterType] = useState<string>('all')
  const [editing, setEditing] = useState('')
  const [creating, setCreating] = useState(false)
  const editingTeam = teams.find((t) => t.id === editing)
  const [storageGiB, setStorageGiB] = useState(200)
  const [modelGiB, setModelGiB] = useState(500)
  const [message, setMessage] = useState('')
  const [busy, setBusy] = useState(false)

  const accounts = usePolling(loadAccounts, 60000)

  const filteredWorkspaces = teams.filter((t) => {
    if (filterType === 'all') return true
    if (filterType === 'team') return !t.type || t.type === 'team'
    if (filterType === 'personal') return t.type === 'personal'
    if (filterType === 'other') return t.type === 'collaboration' || t.type === 'admin'
    return true
  })

  return (
    <main className="px-6 py-8 lg:px-16 space-y-6">
      <div className="mist-page-heading">
        <div>
          <div className="flex items-center gap-2">
            <p className="mist-eyebrow">Attribution & Quotas</p>
            {admin && (
              <span className="bg-amber-100 text-amber-800 text-[10px] font-bold px-1.5 py-0.5 rounded tracking-wide uppercase inline-flex items-center gap-0.5">
                <ShieldCheck size={10} /> Admin Management Enabled
              </span>
            )}
          </div>
          <h1 className="text-2xl font-bold">Workspaces</h1>
          <p className="mist-description">
            Attribution contexts for compute jobs, dataset access, and resource allocations.
          </p>
        </div>
        {admin && (
          <button
            type="button"
            className="mist-button mist-button-primary"
            onClick={() => {
              setMessage('')
              setCreating(true)
            }}
          >
            <Plus size={16} aria-hidden="true" />
            New workspace
          </button>
        )}
      </div>

      {/* Filter Tabs */}
      <div className="flex flex-wrap items-center justify-between gap-3 border-b pb-2">
        <div className="flex gap-2">
          {[
            ['all', `All (${teams.length})`],
            ['team', `Teams (${teams.filter((t) => !t.type || t.type === 'team').length})`],
            ['personal', `Personal Quotas (${teams.filter((t) => t.type === 'personal').length})`],
            ['other', `Collaborations & Infra (${teams.filter((t) => t.type === 'collaboration' || t.type === 'admin').length})`],
          ].map(([id, label]) => (
            <button
              key={id}
              type="button"
              className={`text-xs px-3 py-1.5 rounded-full font-medium transition-colors ${
                filterType === id
                  ? 'bg-gray-900 text-white'
                  : 'bg-gray-100 text-gray-700 hover:bg-gray-200'
              }`}
              onClick={() => setFilterType(id)}
            >
              {label}
            </button>
          ))}
        </div>
        {admin && (
          <p className="text-xs text-gray-500">
            {teams.reduce(
              (sum, t) => sum + t.policy.storage_gib + t.policy.model_storage_gib,
              0,
            )}{' '}
            / {storageBudget} GiB total storage allocated
          </p>
        )}
      </div>

      {/* Admin: Create Workspace Modal */}
      {admin && creating && (
        <Modal
          title="Create Compute Workspace"
          closeDisabled={busy}
          onClose={() => {
            setCreating(false)
            setName('')
            setDescription('')
            setAssignedUser('')
            setMessage('')
          }}
        >
          <form
            className="mist-dialog-form space-y-4"
            onSubmit={async (e) => {
              e.preventDefault()
              setBusy(true)
              setMessage('')
              try {
                const t = await teamsAPI.create(
                  name,
                  wsType,
                  description,
                  storageGiB,
                  modelGiB,
                  undefined,
                  assignedUser || (wsType === 'personal' && user ? user.id : undefined),
                )
                if (user && !assignedUser) {
                  await teamsAPI.enroll(t.id, user.id, true)
                }
                setName('')
                setDescription('')
                setAssignedUser('')
                setCreating(false)
                refresh()
                choose(t.id)
                setMessage(
                  'Workspace created. Resource limits and storage are being provisioned.',
                )
              } catch (err) {
                setMessage(errorMessage(err))
              } finally {
                setBusy(false)
              }
            }}
          >
            <div>
              <label htmlFor="team-name">Workspace Name</label>
              <input
                id="team-name"
                className={field}
                required
                maxLength={100}
                placeholder="e.g. Vision Lab or Alice (Personal)"
                value={name}
                onChange={(e) => setName(e.target.value)}
              />
            </div>

            <div>
              <label htmlFor="workspace-type">Workspace Type</label>
              <Picker
                id="workspace-type"
                className={field}
                value={wsType}
                onChange={(e) => {
                  const val = e.target.value as WorkspaceType
                  setWsType(val)
                  if (val === 'personal') {
                    setStorageGiB(50)
                    setModelGiB(50)
                  }
                }}
              >
                <option value="team">Team Workspace (Shared research team)</option>
                <option value="personal">Personal Quota (User-specific allocation)</option>
                <option value="collaboration">Team Collaboration (Cross-team research)</option>
                <option value="admin">Infra Admin Allocation (Infrastructure debugging)</option>
              </Picker>
              <p className="text-xs text-gray-500 mt-1">
                {wsType === 'personal'
                  ? 'Allocates personal computing and storage quota for a single researcher.'
                  : wsType === 'collaboration'
                    ? 'Cross-team collaborative workspace without replacing underlying team memberships.'
                    : wsType === 'admin'
                      ? 'Administrative debugging allocation for cluster testing.'
                      : 'Standard team workspace where all enrolled team members can attribute experiments.'}
              </p>
            </div>

            <div>
              <label htmlFor="workspace-desc">Description (Optional)</label>
              <input
                id="workspace-desc"
                className={field}
                maxLength={500}
                placeholder="Brief purpose of this workspace"
                value={description}
                onChange={(e) => setDescription(e.target.value)}
              />
            </div>

            <div>
              <label htmlFor="workspace-user">Initial Member Assignment (Optional)</label>
              <Picker
                id="workspace-user"
                className={field}
                value={assignedUser}
                onChange={(e) => setAssignedUser(e.target.value)}
              >
                <option value="">Enroll creator ({user?.name || 'You'})</option>
                {accounts.data?.map((m) => (
                  <option key={m.id} value={m.id}>
                    {m.name} ({m.email})
                  </option>
                ))}
              </Picker>
            </div>

            <div>
              <label htmlFor="new-team-storage">
                Dataset allowance (GiB, advisory)
              </label>
              <input
                id="new-team-storage"
                className={field}
                type="number"
                min={1}
                max={1048576}
                required
                value={storageGiB}
                onChange={(e) => setStorageGiB(Number(e.target.value))}
              />
            </div>

            <div>
              <label htmlFor="new-team-models">
                Model allowance (GiB, advisory)
              </label>
              <input
                id="new-team-models"
                type="number"
                className={field}
                required
                min={1}
                max={1048576}
                value={modelGiB}
                onChange={(event) => setModelGiB(Number(event.target.value))}
              />
            </div>

            <p className="mist-output-note">
              Defaults are 200 GiB of datasets and 500 GiB of models/results.
              These are advisory allowances. All workspaces share available disk
              space; common and member folders remain access-controlled.
            </p>

            <button disabled={busy} className="mist-button mist-button-primary w-full justify-center">
              Create workspace
            </button>
            {message && <p role="alert" className="text-red-700 text-xs">{message}</p>}
          </form>
        </Modal>
      )}

      {admin && (
        <p className="mist-output-note mb-4">
          Storage allowances are advisory and do not reserve disk space. Manage accounts in Account → Members.
        </p>
      )}

      {message && !creating && <p role="status" className="text-sm text-green-700">{message}</p>}

      {filteredWorkspaces.length === 0 && (
        <div className="border border-dashed rounded-lg p-8 text-center text-sm text-gray-500">
          No workspaces match this filter.
          {!admin && ' An administrator can grant you membership to team or personal workspaces.'}
        </div>
      )}

      {/* Workspaces Grid */}
      <div className="mist-team-grid">
        {filteredWorkspaces.map((t) => {
          const badge = workspaceTypeBadge(t.type)
          const isSelected = selected === t.id
          const isMember = t.members.some((m) => m.id === user?.id)

          return (
            <section
              key={t.id}
              className={`mist-team-card mist-card border p-5 relative transition-all ${
                isSelected
                  ? 'border-gray-900 shadow-md ring-1 ring-gray-900'
                  : 'border-gray-200'
              }`}
            >
              <div className="flex items-start justify-between gap-2 mb-2">
                <span className={`text-[11px] px-2 py-0.5 rounded border font-semibold ${badge.className}`}>
                  {badge.icon} {badge.label}
                </span>
                <div className="flex items-center gap-1.5">
                  {isSelected && (
                    <span className="text-[10px] bg-gray-900 text-white font-bold px-1.5 py-0.5 rounded uppercase tracking-wider inline-flex items-center gap-1">
                      <Check size={10} /> Active
                    </span>
                  )}
                  <span className="mist-badge">
                    {t.disabled ? 'Disabled' : 'Ready'}
                  </span>
                </div>
              </div>

              <h2 className="text-lg font-bold text-gray-900">{t.name}</h2>
              {t.description && (
                <p className="text-xs text-gray-600 line-clamp-2 mt-0.5">{t.description}</p>
              )}
              <p className="text-xs text-gray-500 mt-1">
                {t.members.length} {t.members.length === 1 ? 'member' : 'members'}
                {isMember && ' · You are a member'}
              </p>

              <div className="mist-team-card-stats my-3">
                <span>
                  <strong>{t.policy.cpu} cores</strong> CPU
                </span>
                <span>
                  <strong>{t.policy.nvidia}</strong> NVIDIA
                </span>
                <span>
                  <strong>{t.policy.tenstorrent}</strong> TT
                </span>
                <span>
                  <strong>{t.policy.storage_gib} GiB</strong> datasets
                </span>
              </div>

              <div className="mist-team-card-actions pt-2 border-t">
                <button
                  disabled={t.disabled || (!admin && !isMember)}
                  onClick={() => choose(t.id)}
                  className={`text-xs font-semibold px-3 py-1.5 rounded transition-colors inline-flex items-center gap-1 ${
                    isSelected
                      ? 'bg-gray-100 text-gray-800'
                      : 'bg-gray-900 hover:bg-gray-800 text-white'
                  }`}
                >
                  {isSelected ? 'Active workspace' : 'Select workspace'}{' '}
                  <ArrowUpRight size={13} aria-hidden="true" />
                </button>
                {admin && (
                  <button
                    type="button"
                    aria-label={`Manage ${t.name}`}
                    className="text-xs border border-gray-300 hover:bg-gray-100 px-2.5 py-1.5 rounded font-medium inline-flex items-center gap-1 text-gray-700"
                    onClick={() => setEditing(t.id)}
                  >
                    <Settings2 size={13} aria-hidden="true" /> Manage
                  </button>
                )}
              </div>
            </section>
          )
        })}
      </div>

      {admin && editingTeam && (
        <Modal title="Workspace Configuration" onClose={() => setEditing('')}>
          <WorkspaceAdministration
            team={editingTeam}
            teams={teams}
            refresh={refresh}
          />
        </Modal>
      )}
    </main>
  )
}
export const WorkspacesPage = TeamsPage

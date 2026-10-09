import { Picker } from '#/components/Picker.tsx'
import { useState, useEffect } from 'react'
import { Modal } from '#/components/Modal.tsx'
import { Tabs } from '#/components/Tabs.tsx'
import { Users, Settings2, ArrowUpRight, Plus, UserMinus } from 'lucide-react'
import { PanelHeading } from '#/components/Card.tsx'
import type { Team, TeamPolicy } from '#/api.ts'
import { teamsAPI } from '#/api.ts'
import { authClient, useAccount } from '#/auth.tsx'
import { useTeam } from '#/teams.tsx'
import { errorMessage, usePolling } from '#/hooks/usePolling.ts'

const field = 'border rounded p-2 w-full'
async function loadAccounts() {
  const result = await authClient.admin.listUsers({ query: { limit: 1000 } })
  if (result.error) throw new Error(result.error.message)
  return result.data.users
}

function PolicyForm({
  team,
  save,
}: {
  team: Team
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
    ['storage_gib', 'Storage allocation (GiB)', team.policy.storage_gib, 80],
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
          setMessage('Team limits saved.')
        } catch (err) {
          setMessage(errorMessage(err))
        } finally {
          setBusy(false)
        }
      }}
    >
      <h3 className="font-semibold">Team limits</h3>
      <div className="grid md:grid-cols-3 gap-3">
        <div>
          <label htmlFor={`${team.id}-cpu`}>Total CPU cores</label>
          <input
            id={`${team.id}-cpu`}
            className={field}
            required
            value={policy.cpu}
            onChange={(e) => setPolicy({ ...policy, cpu: e.target.value })}
          />
        </div>
        <div>
          <label htmlFor={`${team.id}-memory`}>
            Total memory (for example 16Gi)
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
            <label htmlFor={`${team.id}-${key}`}>{name}</label>
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
              className="mist-range w-full mt-3"
            />
          </div>
        ))}
      </div>
      <div>
        <label htmlFor={`${team.id}-registries`}>
          Allowed container registries (one per line)
        </label>
        <textarea
          id={`${team.id}-registries`}
          className={field}
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
      <p className="text-sm">
        Limits apply to the team as a whole. Storage can be expanded; existing
        storage cannot be shrunk online. Jobs wait when the team or machines are
        busy.
      </p>
      <button disabled={busy} className="bg-green-200 rounded p-2">
        Save limits
      </button>
      {message && <p role="status">{message}</p>}
    </form>
  )
}

function TeamAdministration({
  team,
  teams,
  refresh,
}: {
  team: Team
  teams: Team[]
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
  return (
    <section className="border rounded p-6 space-y-5">
      <div className="flex items-start justify-between gap-4">
        <PanelHeading
          title={team.name}
          description={`${team.members.length} teammates · ${team.policy.storage_gib} GiB storage${team.disabled ? ' · Disabled' : ''}`}
          icon={<Users size={20} />}
        />
        <button
          disabled={busy}
          onClick={() =>
            void action(() =>
              teamsAPI.update(team.id, { disabled: !team.disabled }),
            )
          }
        >
          {team.disabled ? 'Enable team' : 'Disable team'}
        </button>
      </div>
      <Tabs
        id="team-settings"
        label="Team settings"
        value={tab}
        onChange={setTab}
        items={[
          ['members', 'Teammates'],
          ['limits', 'Limits'],
          ['sharing', 'Sharing'],
        ]}
      />
      <div
        role="tabpanel"
        id="team-settings-members-panel"
        aria-labelledby="team-settings-members-tab"
        hidden={tab !== 'members'}
      >
        <form
          className="flex flex-wrap justify-between gap-3 items-end mb-5"
          onSubmit={(e) => {
            e.preventDefault()
            void action(() => teamsAPI.enroll(team.id, userID, commonWriter))
          }}
        >
          <div className="basis-full min-w-0">
            <label htmlFor={`${team.id}-member`}>Existing Mist account</label>
            <Picker
              id={`${team.id}-member`}
              className={field}
              required
              value={userID}
              onChange={(e) => setUserID(e.target.value)}
            >
              <option value="">Select account</option>
              {accounts.data
                ?.filter((m) => !m.banned)
                .map((m) => (
                  <option key={m.id} value={m.id}>
                    {m.name} · {m.email}
                  </option>
                ))}
            </Picker>
          </div>
          <label className="flex gap-2">
            <input
              type="checkbox"
              checked={commonWriter}
              onChange={(e) => setCommonWriter(e.target.checked)}
            />
            Can write common folder
          </label>
          <button
            disabled={busy || !userID}
            className="bg-green-200 rounded p-2"
          >
            Add/update teammate
          </button>
        </form>
        {accounts.error && (
          <p role="alert">Accounts unavailable: {accounts.error}</p>
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
                aria-label={`Remove ${m.name} from team`}
                title="Remove teammate"
                disabled={busy}
                onClick={() =>
                  void action(() => teamsAPI.removeMember(team.id, m.id))
                }
              >
                <UserMinus size={15} aria-hidden="true" />
              </button>
            </li>
          ))}
        </ul>
      </div>
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
      <section
        role="tabpanel"
        id="team-settings-sharing-panel"
        aria-labelledby="team-settings-sharing-tab"
        hidden={tab !== 'sharing'}
        className="border-t pt-4 space-y-3"
      >
        <h3 className="font-semibold">Share a folder with another team</h3>
        <p className="text-sm">
          Recipients can browse/download files and use datasets as read-only job
          inputs. They cannot change the source team's files.
        </p>
        <form
          className="grid md:grid-cols-3 gap-3"
          onSubmit={(e) => {
            e.preventDefault()
            void action(() => teamsAPI.grant(team.id, target, scope))
          }}
        >
          <div>
            <label htmlFor={`${team.id}-scope`}>Folder</label>
            <Picker
              id={`${team.id}-scope`}
              className={field}
              value={scope}
              onChange={(e) => setScope(e.target.value)}
            >
              <option value="common">Common</option>
              {team.members.map((m) => (
                <option key={m.id} value={m.id}>
                  {m.name}
                </option>
              ))}
            </Picker>
          </div>
          <div>
            <label htmlFor={`${team.id}-target`}>Recipient team</label>
            <Picker
              id={`${team.id}-target`}
              className={field}
              required
              value={target}
              onChange={(e) => setTarget(e.target.value)}
            >
              <option value="">Select team</option>
              {teams
                .filter((t) => t.id !== team.id && !t.disabled)
                .map((t) => (
                  <option key={t.id} value={t.id}>
                    {t.name}
                  </option>
                ))}
            </Picker>
          </div>
          <button
            disabled={busy || !target}
            className="bg-green-200 rounded p-2 self-end"
          >
            Grant read/use access
          </button>
        </form>
        <ul>
          {team.grants.map((g) => (
            <li key={g.id} className="flex justify-between gap-3 border-t py-2">
              <span>
                {g.scope === 'common'
                  ? 'Common'
                  : (team.members.find((m) => m.id === g.scope)?.name ??
                    'Former member')}{' '}
                →{' '}
                {teams.find((t) => t.id === g.target_team)?.name ??
                  g.target_team}
              </span>
              <button
                disabled={busy}
                onClick={() =>
                  void action(() => teamsAPI.revoke(team.id, g.id))
                }
              >
                Revoke access
              </button>
            </li>
          ))}
        </ul>
      </section>
      {error && (
        <p role="alert" className="text-red-700">
          {error}
        </p>
      )}
    </section>
  )
}

export function TeamsPage() {
  const { user } = useAccount()
  const { teams, refresh, choose } = useTeam()
  const admin = user?.role.split(',').includes('admin')
  const [name, setName] = useState('')
  const [editing, setEditing] = useState('')
  const [creating, setCreating] = useState(false)
  const editingTeam = teams.find((t) => t.id === editing)
  const [storageGiB, setStorageGiB] = useState(10)
  const [message, setMessage] = useState('')
  const [busy, setBusy] = useState(false)
  return (
    <main className="px-6 py-8 lg:px-16 space-y-6">
      <div className="mist-page-heading">
        <div>
          <p className="mist-eyebrow">Shared research</p>
          <h1 className="text-2xl font-bold">Teams</h1>
          <p className="mist-description">
            Your people, experiments and shared files in one workspace.
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
            New team
          </button>
        )}
      </div>
      {admin && creating && (
        <Modal
          title="Create team"
          closeDisabled={busy}
          onClose={() => {
            setCreating(false)
            setName('')
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
                const t = await teamsAPI.create(name, storageGiB)
                if (user) await teamsAPI.enroll(t.id, user.id, true)
                setName('')
                setCreating(false)
                refresh()
                choose(t.id)
                setMessage(
                  'Team created. Storage is being prepared; use Manage to add teammates.',
                )
              } catch (err) {
                setMessage(errorMessage(err))
              } finally {
                setBusy(false)
              }
            }}
          >
            <div className="grow">
              <label htmlFor="team-name">New team name</label>
              <input
                id="team-name"
                className={field}
                required
                maxLength={100}
                value={name}
                onChange={(e) => setName(e.target.value)}
              />
            </div>
            <div>
              <label htmlFor="new-team-storage">Storage (GiB)</label>
              <input
                id="new-team-storage"
                className={field}
                type="number"
                min={1}
                max={80}
                required
                value={storageGiB}
                onChange={(e) => setStorageGiB(Number(e.target.value))}
              />
            </div>
            <input
              aria-label="Initial storage slider"
              type="range"
              min={1}
              max={80}
              step={1}
              value={storageGiB}
              onChange={(e) => setStorageGiB(Number(e.target.value))}
              className="mist-range w-full"
            />
            <p className="mist-output-note">
              Storage is shared by common and personal folders. You can expand
              it later within the 80 GiB allocation budget.
            </p>
            <button disabled={busy} className="mist-button mist-button-primary">
              Create team
            </button>
            {message && <p role="alert">{message}</p>}
          </form>
        </Modal>
      )}
      {admin && (
        <p className="mist-output-note">
          {teams.reduce((sum, t) => sum + t.policy.storage_gib, 0)} / 80 GiB
          allocated · Manage accounts in Account → Members.
        </p>
      )}
      {message && !creating && <p role="status">{message}</p>}
      {teams.length === 0 && (
        <p>No team memberships yet. An administrator can add your account.</p>
      )}
      <div className="mist-team-grid">
        {teams.map((t) => (
          <section key={t.id} className="mist-team-card mist-card border p-5">
            <div className="mist-team-card-top">
              <span className="mist-panel-icon">
                <Users size={20} />
              </span>
              <span className="mist-badge">
                {t.disabled ? 'Disabled' : 'Active'}
              </span>
            </div>
            <h2>{t.name}</h2>
            <p>{t.members.length} teammates</p>
            <div className="mist-team-card-stats">
              <span>
                <strong>{t.policy.storage_gib} GiB</strong> storage
              </span>
              <span>
                <strong>{t.policy.concurrent}</strong> parallel jobs
              </span>
            </div>
            <div className="mist-team-card-actions">
              <button
                disabled={
                  t.disabled || !t.members.some((m) => m.id === user?.id)
                }
                onClick={() => choose(t.id)}
              >
                Open workspace <ArrowUpRight size={14} aria-hidden="true" />
              </button>
              {admin && (
                <button
                  type="button"
                  aria-label={`Manage ${t.name}`}
                  onClick={() => setEditing(t.id)}
                >
                  <Settings2 size={14} aria-hidden="true" /> Manage
                </button>
              )}
            </div>
          </section>
        ))}
      </div>
      {admin && editingTeam && (
        <Modal title="Team settings" onClose={() => setEditing('')}>
          <TeamAdministration
            team={editingTeam}
            teams={teams}
            refresh={refresh}
          />
        </Modal>
      )}
    </main>
  )
}

import { Picker } from '#/components/Picker.tsx'
import { Modal } from '#/components/Modal.tsx'
import { Tabs } from '#/components/Tabs.tsx'
import { useState } from 'react'
import { Users, KeyRound, Plus, Search } from 'lucide-react'
import { PanelHeading } from '#/components/Card.tsx'
import { authClient, useAccount } from '#/auth.tsx'
import { errorMessage, usePolling } from '#/hooks/usePolling.ts'

const field = 'border rounded p-2 w-full'
function PasswordReset({
  users,
  initialUserId = '',
}: {
  initialUserId?: string
  users: { id: string; name: string; email: string }[]
}) {
  const [userId, setUserId] = useState(initialUserId)
  const [password, setPassword] = useState('')
  const [message, setMessage] = useState('')
  const [busy, setBusy] = useState(false)
  return (
    <form
      className="mist-dialog-form space-y-4"
      onSubmit={async (e) => {
        e.preventDefault()
        setBusy(true)
        setMessage('')
        try {
          const reset = await authClient.admin.setUserPassword({
            userId,
            newPassword: password,
          })
          if (reset.error) throw new Error(reset.error.message)
          const revoke = await authClient.admin.revokeUserSessions({ userId })
          if (revoke.error)
            throw new Error(
              `Password changed; session revocation needs retry: ${revoke.error.message}`,
            )
          setPassword('')
          setMessage(
            'Password reset and existing sessions signed out. Share the new password privately.',
          )
        } catch (err) {
          setMessage(errorMessage(err))
        } finally {
          setBusy(false)
        }
      }}
    >
      <div>
        <label htmlFor="reset-account">Account to reset</label>
        <Picker
          id="reset-account"
          className={field}
          required
          value={userId}
          onChange={(e) => setUserId(e.target.value)}
        >
          <option value="">Select account</option>
          {users.map((u) => (
            <option key={u.id} value={u.id}>
              {u.name} · {u.email}
            </option>
          ))}
        </Picker>
      </div>
      <div>
        <label htmlFor="reset-password">New temporary password</label>
        <input
          id="reset-password"
          className={field}
          type="password"
          autoComplete="new-password"
          required
          minLength={12}
          maxLength={128}
          value={password}
          onChange={(e) => setPassword(e.target.value)}
        />
      </div>
      <button disabled={busy || !userId} className="bg-green-200 rounded p-2">
        Reset password and sign out sessions
      </button>
      {message && <p role="status">{message}</p>}
    </form>
  )
}
async function listMembers() {
  const result = await authClient.admin.listUsers({ query: { limit: 1000 } })
  if (result.error) throw new Error(result.error.message)
  return result.data
}
function Members() {
  const members = usePolling(listMembers, 30000)
  const [name, setName] = useState(''),
    [email, setEmail] = useState(''),
    [password, setPassword] = useState(''),
    [error, setError] = useState(''),
    [busy, setBusy] = useState(false)
  const current = useAccount().user
  const [creating, setCreating] = useState(false)
  const [resetUser, setResetUser] = useState<string | null>(null)
  const [search, setSearch] = useState('')
  const [page, setPage] = useState(1)
  const filtered = (members.data?.users ?? []).filter((member) =>
    `${member.name} ${member.email}`
      .toLowerCase()
      .includes(search.toLowerCase()),
  )
  const totalPages = Math.max(1, Math.ceil(filtered.length / 10))
  const currentPage = Math.min(page, totalPages)
  const visibleMembers = filtered.slice(
    (currentPage - 1) * 10,
    currentPage * 10,
  )
  function closeCreate() {
    setCreating(false)
    setName('')
    setEmail('')
    setPassword('')
    setError('')
  }
  async function access(id: string, banned: boolean) {
    setError('')
    setBusy(true)
    try {
      const result = banned
        ? await authClient.admin.unbanUser({ userId: id })
        : await authClient.admin.banUser({
            userId: id,
            banReason: 'Deactivated by Mist administrator',
          })
      if (result.error) throw new Error(result.error.message)
      members.refresh()
    } catch (err) {
      setError(errorMessage(err))
    } finally {
      setBusy(false)
    }
  }
  return (
    <section className="mist-card mist-members-panel border rounded p-6 space-y-4">
      <PanelHeading
        title="Members"
        description="Create accounts and manage access to the portal."
        icon={<Users size={20} />}
        action={
          <button
            type="button"
            className="mist-button mist-button-primary"
            onClick={() => setCreating(true)}
          >
            <Plus size={15} aria-hidden="true" />
            New member
          </button>
        }
      />
      {creating && (
        <Modal title="Add member" closeDisabled={busy} onClose={closeCreate}>
          <form
            className="mist-dialog-form space-y-4"
            onSubmit={async (e) => {
              e.preventDefault()
              setBusy(true)
              setError('')
              try {
                const result = await authClient.admin.createUser({
                  name,
                  email,
                  password,
                  role: 'user',
                })
                if (result.error) throw new Error(result.error.message)
                setName('')
                setEmail('')
                setPassword('')
                members.refresh()
                setCreating(false)
              } catch (err) {
                setError(errorMessage(err))
              } finally {
                setBusy(false)
              }
            }}
          >
            <div>
              <label htmlFor="member-name">Name</label>
              <input
                id="member-name"
                className={field}
                required
                value={name}
                onChange={(e) => setName(e.target.value)}
              />
            </div>
            <div>
              <label htmlFor="member-email">Email</label>
              <input
                id="member-email"
                type="email"
                className={field}
                required
                value={email}
                onChange={(e) => setEmail(e.target.value)}
              />
            </div>
            <div>
              <label htmlFor="member-password">Initial password</label>
              <input
                id="member-password"
                type="password"
                autoComplete="new-password"
                minLength={12}
                maxLength={128}
                className={field}
                required
                value={password}
                onChange={(e) => setPassword(e.target.value)}
              />
            </div>
            <button
              disabled={busy}
              className="bg-green-200 rounded p-2 self-end"
            >
              Add member
            </button>
            <p className="mist-output-note">
              Share the initial password privately. Members can change it after
              signing in.
            </p>
            {error && <p role="alert">{error}</p>}
          </form>
        </Modal>
      )}
      <div className="mist-member-toolbar">
        <label className="mist-search-field">
          <Search size={15} aria-hidden="true" />
          <input
            aria-label="Search members"
            placeholder="Find a teammate…"
            value={search}
            onChange={(event) => {
              setSearch(event.target.value)
              setPage(1)
            }}
          />
        </label>
        <span className="mist-badge">{members.data?.total ?? 0} accounts</span>
      </div>
      {(error || members.error) && (
        <p role="alert" className="text-red-700">
          {error || members.error}
        </p>
      )}
      <ul className="mist-member-list">
        {visibleMembers.map((member) => (
          <li key={member.id}>
            <div className="mist-member-identity">
              <strong>{member.name}</strong>
              <span>{member.email}</span>
            </div>
            <span className="mist-badge">
              {member.banned
                ? 'Deactivated'
                : member.role === 'admin'
                  ? 'Admin'
                  : 'Member'}
            </span>
            <div className="mist-member-actions">
              <button
                type="button"
                aria-label={`Reset password for ${member.name}`}
                title="Reset password"
                onClick={() => setResetUser(member.id)}
              >
                <KeyRound size={15} aria-hidden="true" />
              </button>
              {member.id !== current?.id && (
                <button
                  type="button"
                  disabled={busy}
                  onClick={() => void access(member.id, !!member.banned)}
                >
                  {member.banned ? 'Reactivate' : 'Deactivate'}
                </button>
              )}
            </div>
          </li>
        ))}
      </ul>
      {!members.loading && !filtered.length && (
        <p className="mist-empty-state">
          {search ? 'No members match your search.' : 'No members yet.'}
        </p>
      )}
      {members.loading && !members.data && (
        <p role="status" className="mist-help">
          Loading members…
        </p>
      )}
      {totalPages > 1 && (
        <div className="mist-pagination">
          <span>{filtered.length} accounts</span>
          <div>
            <button
              aria-label="Previous members page"
              disabled={currentPage === 1}
              onClick={() => setPage(currentPage - 1)}
            >
              Previous
            </button>
            <span role="status">
              Page {currentPage} of {totalPages}
            </span>
            <button
              aria-label="Next members page"
              disabled={currentPage === totalPages}
              onClick={() => setPage(currentPage + 1)}
            >
              Next
            </button>
          </div>
        </div>
      )}
      <button
        type="button"
        className="mist-button"
        onClick={() => setResetUser('')}
      >
        <KeyRound size={15} aria-hidden="true" />
        Reset password
      </button>
      {resetUser !== null && (
        <Modal title="Reset password" onClose={() => setResetUser(null)}>
          <PasswordReset
            initialUserId={resetUser}
            users={members.data?.users ?? []}
          />
        </Modal>
      )}
    </section>
  )
}
export function AccountPage() {
  const account = useAccount()
  const [tab, setTab] = useState('personal')
  const isAdmin = account.user?.role.split(',').includes('admin')
  const [current, setCurrent] = useState(''),
    [next, setNext] = useState(''),
    [message, setMessage] = useState(''),
    [busy, setBusy] = useState(false)
  if (!account.enabled)
    return <p className="p-8">Member login is disabled in this local pilot.</p>
  return (
    <main className="px-6 py-8 lg:px-16 space-y-6">
      <div className="mist-page-heading">
        <div>
          <p className="mist-eyebrow">
            {isAdmin ? 'Administrator & teammate' : 'Your workspace'}
          </p>
          <h1 className="text-2xl font-bold">Account</h1>
          <p className="mist-description">
            {account.user?.name} · {account.user?.email}
          </p>
        </div>
      </div>
      {isAdmin && (
        <Tabs
          id="account-settings"
          label="Account settings"
          value={tab}
          onChange={setTab}
          items={[
            ['personal', 'My account'],
            ['members', 'Members'],
          ]}
        />
      )}
      <div
        role="tabpanel"
        id="account-settings-personal-panel"
        aria-labelledby={isAdmin ? 'account-settings-personal-tab' : undefined}
        hidden={tab !== 'personal'}
      >
        <section className="mist-card border rounded p-6 max-w-xl">
          <PanelHeading
            title="Change password"
            description="Keep your account secure. Other sessions will be signed out."
            icon={<KeyRound size={20} />}
          />
          <form
            className="space-y-3"
            onSubmit={async (e) => {
              e.preventDefault()
              setBusy(true)
              setMessage('')
              try {
                const result = await authClient.changePassword({
                  currentPassword: current,
                  newPassword: next,
                  revokeOtherSessions: true,
                })
                if (result.error) throw new Error(result.error.message)
                setCurrent('')
                setNext('')
                setMessage('Password changed. Other sessions were signed out.')
              } catch (err) {
                setMessage(errorMessage(err))
              } finally {
                setBusy(false)
              }
            }}
          >
            <div>
              <label htmlFor="current-password">Current password</label>
              <input
                id="current-password"
                type="password"
                autoComplete="current-password"
                required
                className={field}
                value={current}
                onChange={(e) => setCurrent(e.target.value)}
              />
            </div>
            <div>
              <label htmlFor="new-password">New password</label>
              <input
                id="new-password"
                type="password"
                autoComplete="new-password"
                required
                minLength={12}
                maxLength={128}
                className={field}
                value={next}
                onChange={(e) => setNext(e.target.value)}
              />
            </div>
            <button disabled={busy} className="bg-green-200 rounded p-2">
              Change password
            </button>
            {message && <p role="status">{message}</p>}
          </form>
        </section>
      </div>
      {isAdmin && (
        <div
          role="tabpanel"
          id="account-settings-members-panel"
          aria-labelledby="account-settings-members-tab"
          hidden={tab !== 'members'}
        >
          <Members />
        </div>
      )}
    </main>
  )
}

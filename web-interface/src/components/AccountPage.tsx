import { useState } from 'react'
import { authClient, useAccount } from '#/auth.tsx'
import { errorMessage, usePolling } from '#/hooks/usePolling.ts'

const field = 'border rounded p-2 w-full'
async function listMembers() {
  const result = await authClient.admin.listUsers({ query: { limit: 100 } })
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
  async function access(id: string, banned: boolean) {
    setError('')
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
    }
  }
  return (
    <section className="border rounded p-6 space-y-4">
      <h2 className="text-xl font-semibold">Members</h2>
      <form
        className="grid md:grid-cols-4 gap-3"
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
        <button disabled={busy} className="bg-green-200 rounded p-2 self-end">
          Add member
        </button>
      </form>
      <p className="text-sm">
        Share the initial password privately. Members can change it below.
        Public signup is disabled.
      </p>
      {(error || members.error) && (
        <p role="alert" className="text-red-700">
          {error || members.error}
        </p>
      )}
      <ul className="space-y-2">
        {members.data?.users.map((member) => (
          <li
            key={member.id}
            className="flex justify-between gap-4 border-t py-2"
          >
            <span>
              {member.name} · {member.email} · {member.role}
              {member.banned ? ' · Deactivated' : ''}
            </span>
            {member.id !== current?.id && (
              <button onClick={() => void access(member.id, !!member.banned)}>
                {member.banned ? 'Reactivate' : 'Deactivate'}
              </button>
            )}
          </li>
        ))}
      </ul>
    </section>
  )
}
export function AccountPage() {
  const account = useAccount()
  const [current, setCurrent] = useState(''),
    [next, setNext] = useState(''),
    [message, setMessage] = useState(''),
    [busy, setBusy] = useState(false)
  if (!account.enabled)
    return <p className="p-8">Member login is disabled in this local pilot.</p>
  return (
    <main className="px-6 py-8 lg:px-16 space-y-6">
      <h1 className="text-2xl font-bold">Account</h1>
      <p>
        {account.user?.name} · {account.user?.email} · {account.user?.role}
      </p>
      <section className="border rounded p-6 max-w-xl">
        <h2 className="text-xl font-semibold mb-4">Change password</h2>
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
      {account.user?.role.split(',').includes('admin') && <Members />}
    </main>
  )
}

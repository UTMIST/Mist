import { createContext, useContext, useState, useEffect } from 'react'
import type { ReactNode } from 'react'
import { createAuthClient } from 'better-auth/react'
import { adminClient } from 'better-auth/client/plugins'
import { jobsAPI } from '#/api.ts'
import { errorMessage, usePolling } from '#/hooks/usePolling.ts'

export const authClient = createAuthClient({
  basePath: '/auth',
  plugins: [adminClient()],
})
export type Member = {
  id: string
  name: string
  email: string
  role: string
  banned?: boolean
}
const AccountContext = createContext<{
  user: Member | null
  enabled: boolean
  refresh: () => void
}>({ user: null, enabled: false, refresh: () => {} })
export const useAccount = () => useContext(AccountContext)

function Login({ refresh }: { refresh: () => void }) {
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  return (
    <main className="mist-login max-w-md mx-auto mt-16 p-8 border rounded-xl">
      <p className="mist-eyebrow">MIST / RESEARCH COMPUTE</p>
      <h1 className="text-2xl font-bold mb-2">Sign in to Mist</h1>
      <p className="mb-6 text-gray-600">
        Run jobs on the team’s compute machines. Ask a Mist administrator for an
        account.
      </p>
      <form
        className="space-y-4"
        onSubmit={async (e) => {
          e.preventDefault()
          setBusy(true)
          setError('')
          try {
            const result = await authClient.signIn.email({ email, password })
            if (result.error) throw new Error(result.error.message)
            setPassword('')
            refresh()
          } catch (err) {
            setError(errorMessage(err))
          } finally {
            setBusy(false)
          }
        }}
      >
        <div>
          <label htmlFor="login-email">Email</label>
          <input
            id="login-email"
            type="email"
            required
            autoComplete="username"
            className="w-full border rounded p-2"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
          />
        </div>
        <div>
          <label htmlFor="login-password">Password</label>
          <input
            id="login-password"
            type="password"
            required
            autoComplete="current-password"
            className="w-full border rounded p-2"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
          />
        </div>
        {error && (
          <p role="alert" className="text-red-700">
            {error}
          </p>
        )}
        <button
          disabled={busy}
          className="mist-primary-link w-full justify-center"
        >
          {busy ? 'Signing in…' : 'Sign in'}
        </button>
      </form>
    </main>
  )
}
export function AccountGate({ children }: { children: ReactNode }) {
  const session = usePolling(jobsAPI.session, 60000)
  useEffect(() => {
    window.addEventListener('mist:sign-in-required', session.refresh)
    return () =>
      window.removeEventListener('mist:sign-in-required', session.refresh)
  }, [session.refresh])
  if (session.loading && !session.data)
    return (
      <p className="p-8" role="status">
        Loading account…
      </p>
    )
  if (session.error)
    return (
      <main className="p-8">
        <p role="alert">Account service unavailable: {session.error}</p>
        <button onClick={session.refresh}>Retry</button>
      </main>
    )
  if (session.data?.enabled && !session.data.user)
    return <Login refresh={session.refresh} />
  return (
    <AccountContext
      value={{
        user: session.data?.user ?? null,
        enabled: session.data?.enabled ?? false,
        refresh: session.refresh,
      }}
    >
      {children}
    </AccountContext>
  )
}

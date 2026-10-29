import { useEffect, useState } from 'react'
import { errorMessage } from '#/hooks/usePolling.ts'

async function loginRequest(path: string, body: object) {
  const response = await fetch(`/auth/misty/${path}`, {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  const data = await response.json()
  if (!response.ok)
    throw new Error(data.message || 'Sign-in is temporarily unavailable.')
  return data
}

export function MistyLogin({ refresh }: { refresh: () => void }) {
  const [email, setEmail] = useState('')
  const [code, setCode] = useState('')
  const [challenge, setChallenge] = useState('')
  const [requests, setRequests] = useState<Array<{ id: string; at: number }>>(
    [],
  )
  const [message, setMessage] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [cooldown, setCooldown] = useState(0)
  useEffect(() => {
    if (cooldown <= 0) return
    const timer = window.setTimeout(
      () => setCooldown((value) => value - 1),
      1000,
    )
    return () => window.clearTimeout(timer)
  }, [cooldown])

  async function sendCode() {
    setBusy(true)
    setError('')
    try {
      const result = await loginRequest('start', { email })
      setChallenge(result.challenge_id)
      setRequests((previous) =>
        [
          { id: result.challenge_id, at: Date.now() },
          ...previous.filter((request) => Date.now() - request.at < 600000),
        ].slice(0, 10),
      )
      setCode('')
      setMessage(
        'If your email belongs to an active Misty account, a code will arrive shortly. It expires in 10 minutes.',
      )
      setCooldown(60)
    } catch (err) {
      setError(errorMessage(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <main className="mist-login max-w-md mx-auto mt-16 p-8 border rounded-xl">
      <p className="mist-eyebrow">MIST / RESEARCH COMPUTE</p>
      <h1 className="text-2xl font-bold mb-2">Sign in with Misty</h1>
      <p className="mb-6 text-gray-600">
        Use the email registered on your active Misty account.
      </p>
      <form
        className="space-y-4"
        onSubmit={async (event) => {
          event.preventDefault()
          if (!challenge) {
            await sendCode()
            return
          }
          setBusy(true)
          setError('')
          try {
            await loginRequest('confirm', { challenge_id: challenge, code })
            setCode('')
            refresh()
          } catch (err) {
            setError(errorMessage(err))
          } finally {
            setBusy(false)
          }
        }}
      >
        <div>
          <label htmlFor="misty-email">Email</label>
          <input
            id="misty-email"
            type="email"
            required
            autoComplete="email"
            className="w-full border rounded p-2"
            value={email}
            readOnly={Boolean(challenge)}
            onChange={(event) => setEmail(event.target.value)}
          />
        </div>
        {challenge && (
          <div>
            {requests.length > 1 && (
              <div className="mb-3">
                <label htmlFor="misty-request">Code request</label>
                <select
                  id="misty-request"
                  className="w-full border rounded p-2"
                  value={challenge}
                  onChange={(event) => {
                    setChallenge(event.target.value)
                    setCode('')
                    setError('')
                  }}
                >
                  {requests.map((request, index) => (
                    <option key={request.id} value={request.id}>
                      {index === 0 ? 'Latest request' : 'Earlier request'} —{' '}
                      {new Date(request.at).toLocaleTimeString()}
                    </option>
                  ))}
                </select>
                <p>
                  If a replacement email has not arrived, select the earlier
                  request to try its code.
                </p>
              </div>
            )}
            <label htmlFor="misty-code">Verification code</label>
            <input
              id="misty-code"
              type="text"
              inputMode="numeric"
              autoComplete="one-time-code"
              pattern="[0-9]{6}"
              maxLength={6}
              required
              autoFocus
              className="w-full border rounded p-2"
              value={code}
              onChange={(event) => setCode(event.target.value)}
            />
          </div>
        )}
        {message && <p role="status">{message}</p>}
        {error && (
          <p role="alert" className="text-red-700">
            {error}
          </p>
        )}
        <button
          disabled={busy}
          className="mist-primary-link w-full justify-center"
        >
          {busy
            ? 'Please wait…'
            : challenge
              ? 'Verify and sign in'
              : 'Send code'}
        </button>
      </form>
      {challenge && (
        <div className="flex gap-4 mt-4">
          <button
            type="button"
            disabled={busy || cooldown > 0}
            onClick={sendCode}
          >
            {cooldown > 0 ? `Resend in ${cooldown}s` : 'Resend code'}
          </button>
          <button
            type="button"
            disabled={busy}
            onClick={() => {
              setChallenge('')
              setRequests([])
              setCode('')
              setMessage('')
              setError('')
            }}
          >
            Use another email
          </button>
        </div>
      )}
    </main>
  )
}

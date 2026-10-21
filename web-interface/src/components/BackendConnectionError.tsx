import { useState } from 'react'
import {
  AlertCircle,
  BookOpen,
  ExternalLink,
  Github,
  RefreshCw,
} from 'lucide-react'

export interface BackendConnectionErrorProps {
  error?: string
  onRetry?: () => void | Promise<void>
  isRetrying?: boolean
}

export function BackendConnectionError({
  error = 'Unable to establish connection to the Mist API service.',
  onRetry,
  isRetrying: externalRetrying,
}: BackendConnectionErrorProps) {
  const [internalRetrying, setInternalRetrying] = useState(false)
  const isRetrying = externalRetrying ?? internalRetrying

  async function handleRetry() {
    if (isRetrying || !onRetry) return
    setInternalRetrying(true)
    try {
      await onRetry()
    } finally {
      setInternalRetrying(false)
    }
  }

  return (
    <div className="min-h-screen bg-[#f8f7f4] flex flex-col justify-center items-center p-4 sm:p-6 md:p-10 font-sans text-[#292b25]">
      <main className="w-full max-w-2xl bg-[#fffefa] border border-[#e5e3db] rounded-xl shadow-xs p-6 sm:p-8 space-y-6">
        {/* Brand header & status badge */}
        <header className="flex flex-wrap items-center justify-between gap-4 pb-5 border-b border-[#e5e3db]">
          <div className="flex items-center gap-3">
            <div
              className="p-2.5 bg-[#30372b] text-[#f7f7ee] rounded-lg shadow-xs"
              aria-hidden="true"
            >
              <svg width="22" height="22" viewBox="0 0 28 28" fill="none">
                <path d="M14 2 26 9 14 16 2 9 14 2Z" fill="currentColor" />
                <path
                  d="m2 14 12 7 12-7M2 19l12 7 12-7"
                  stroke="currentColor"
                  strokeWidth="2"
                />
              </svg>
            </div>
            <div>
              <span className="font-semibold text-base tracking-tight text-[#262722] block leading-snug">
                Mist
              </span>
              <span className="text-xs text-[#77786c]">Research Compute</span>
            </div>
          </div>

          <div
            className="flex items-center gap-2 px-3 py-1 rounded-full text-xs font-medium border border-amber-300 bg-amber-50 text-amber-900"
            role="status"
          >
            <span
              className="w-2 h-2 rounded-full bg-amber-500 animate-pulse"
              aria-hidden="true"
            />
            <span>Backend Offline</span>
          </div>
        </header>

        {/* Primary Message */}
        <section className="space-y-2">
          <p className="mist-eyebrow mb-1">SYSTEM STATUS / API DISCONNECTED</p>
          <h1 className="text-2xl sm:text-3xl font-semibold tracking-tight text-[#262722]">
            Unable to reach the compute backend
          </h1>
          <p className="text-sm text-[#65665c] leading-relaxed">
            The web portal is running, but cannot communicate with the Mist API
            or account service. The backend server may be restarting, undergoing
            maintenance, or temporarily unreachable over the private network.
          </p>
        </section>

        {/* Error diagnostics banner */}
        <section
          aria-label="Connection diagnostics"
          className="p-4 rounded-lg bg-[#f0efe9] border border-[#e5e3db] font-mono text-xs text-[#33352e] space-y-1.5"
        >
          <div className="flex items-start gap-2.5">
            <AlertCircle
              className="w-4 h-4 text-rose-600 shrink-0 mt-0.5"
              aria-hidden="true"
            />
            <div className="overflow-x-auto w-full">
              <p
                role="alert"
                className="font-semibold text-rose-800 break-words"
              >
                {error}
              </p>
              <p className="text-[#77786c] text-[11px] mt-1 font-sans">
                Target: <span className="font-mono">/api/session</span> •
                Status:{' '}
                <span className="font-mono">
                  Service Unavailable / Connection Refused
                </span>
              </p>
            </div>
          </div>
        </section>

        {/* Action button */}
        <section className="flex flex-wrap items-center gap-3 pt-1">
          <button
            type="button"
            onClick={handleRetry}
            disabled={isRetrying || !onRetry}
            aria-label="Try again to connect to the backend"
            className="inline-flex items-center justify-center gap-2.5 px-6 py-2.5 rounded-md font-medium text-xs bg-[#30372b] hover:bg-[#48553b] text-[#f7f7ee] shadow-sm transition-all focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[#526649] active:scale-[0.98] disabled:opacity-50 disabled:cursor-not-allowed cursor-pointer"
          >
            <RefreshCw
              className={`w-3.5 h-3.5 ${isRetrying ? 'animate-spin' : ''}`}
              aria-hidden="true"
            />
            <span>{isRetrying ? 'Checking connection…' : 'Try again'}</span>
          </button>
          <span className="text-xs text-[#77786c]">
            Automatic retries happen in the background.
          </span>
        </section>

        {/* Informational Cards: Docs & GitHub */}
        <section className="grid sm:grid-cols-2 gap-3.5 pt-2">
          {/* Static Documentation link */}
          <a
            href="/docs/"
            className="group flex flex-col justify-between p-4 sm:p-5 rounded-lg border border-[#e5e3db] bg-[#fffefa] hover:border-[#b8c2ab] hover:bg-[#fbfbf8] transition-all no-underline text-inherit"
          >
            <div className="space-y-2">
              <div className="flex items-center justify-between">
                <div
                  className="p-1.5 rounded-md bg-[#e1e9dc] text-[#365031]"
                  aria-hidden="true"
                >
                  <BookOpen className="w-4 h-4" />
                </div>
                <span className="text-[10px] font-semibold uppercase tracking-wider px-2 py-0.5 rounded bg-[#e1e9dc] text-[#365031]">
                  Static & Active
                </span>
              </div>
              <h2 className="text-sm font-semibold text-[#262722] group-hover:text-[#365031] flex items-center justify-between">
                Documentation
                <span
                  aria-hidden="true"
                  className="transition-transform group-hover:translate-x-0.5 text-xs text-[#526649]"
                >
                  ↗
                </span>
              </h2>
              <p className="text-xs text-[#65665c] leading-relaxed">
                The static documentation site remains unaffected and fully
                accessible even when the compute backend is down. Browse
                operating guides, architecture, and setup manuals.
              </p>
            </div>
            <div className="mt-4 pt-2 border-t border-[#f0efe9] text-xs font-medium text-[#526649] flex items-center gap-1 group-hover:underline">
              <span>View documentation</span>
              <span aria-hidden="true">→</span>
            </div>
          </a>

          {/* GitHub Repository Issue link */}
          <a
            href="https://github.com/UTMIST/Mist/issues"
            target="_blank"
            rel="noopener noreferrer"
            className="group flex flex-col justify-between p-4 sm:p-5 rounded-lg border border-[#e5e3db] bg-[#fffefa] hover:border-[#b8c2ab] hover:bg-[#fbfbf8] transition-all no-underline text-inherit"
          >
            <div className="space-y-2">
              <div className="flex items-center justify-between">
                <div
                  className="p-1.5 rounded-md bg-[#f0efe9] text-[#33352e]"
                  aria-hidden="true"
                >
                  <Github className="w-4 h-4" />
                </div>
                <span className="text-[10px] font-semibold uppercase tracking-wider px-2 py-0.5 rounded bg-[#f0efe9] text-[#65665c]">
                  GitHub
                </span>
              </div>
              <h2 className="text-sm font-semibold text-[#262722] group-hover:text-[#365031] flex items-center justify-between">
                Report an Issue
                <ExternalLink
                  className="w-3.5 h-3.5 text-[#77786c]"
                  aria-hidden="true"
                />
              </h2>
              <p className="text-xs text-[#65665c] leading-relaxed">
                Notice an unexpected service outage or API bug? Submit a report
                or check active incidents on the UTMIST GitHub repository.
              </p>
            </div>
            <div className="mt-4 pt-2 border-t border-[#f0efe9] text-xs font-medium text-[#526649] flex items-center gap-1 group-hover:underline">
              <span>Open GitHub issues</span>
              <ExternalLink className="w-3 h-3" aria-hidden="true" />
            </div>
          </a>
        </section>

        {/* Troubleshooting checklist */}
        <footer className="pt-3 border-t border-[#e5e3db] space-y-2">
          <h3 className="text-xs font-semibold text-[#262722]">
            Troubleshooting checklist
          </h3>
          <ul className="text-xs text-[#65665c] space-y-1 list-disc list-inside">
            <li>
              <strong>Private mesh:</strong> Verify that your Tailscale client
              is connected to the UTMIST tailnet.
            </li>
            <li>
              <strong>Local dev:</strong> Ensure backend server is active at{' '}
              <code className="px-1 py-0.5 bg-[#f0efe9] rounded">
                http://127.0.0.1:3000
              </code>
              .
            </li>
            <li>
              <strong>Cluster admin:</strong> Check service logs with{' '}
              <code className="px-1 py-0.5 bg-[#f0efe9] rounded">
                kubectl -n mist-system get pods
              </code>
              .
            </li>
          </ul>
        </footer>
      </main>
    </div>
  )
}

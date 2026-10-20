import { Outlet, createRootRoute } from '@tanstack/react-router'
import { TanStackRouterDevtoolsPanel } from '@tanstack/react-router-devtools'
import { TanStackDevtools } from '@tanstack/react-devtools'

import '../styles.css'
import Navbar from '#/components/Navbar.tsx'
import { AccountGate } from '#/auth.tsx'
import { TeamGate } from '#/teams.tsx'
import { BackendConnectionError } from '#/components/BackendConnectionError.tsx'

export const Route = createRootRoute({
  component: RootComponent,
  errorComponent: ({ error, reset }) => (
    <BackendConnectionError
      error={error instanceof Error ? error.message : String(error)}
      onRetry={reset}
    />
  ),
})

function RootComponent() {
  return (
    <AccountGate>
      <TeamGate>
        <div className="mist-shell">
          <Navbar />
          <div className="mist-content">
            <Outlet />
          </div>
        </div>
        {import.meta.env.DEV && (
          <TanStackDevtools
            config={{
              position: 'bottom-right',
            }}
            plugins={[
              {
                name: 'TanStack Router',
                render: <TanStackRouterDevtoolsPanel />,
              },
            ]}
          />
        )}
      </TeamGate>
    </AccountGate>
  )
}

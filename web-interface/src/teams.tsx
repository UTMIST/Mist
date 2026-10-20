import { Picker } from '#/components/Picker.tsx'
import {
  createContext,
  useContext,
  useLayoutEffect,
  useState,
  useEffect,
} from 'react'
import type { ReactNode } from 'react'
import type { Workspace } from '#/api.ts'
import { setActiveTeam, teamsAPI } from '#/api.ts'
import { useAccount, isUserAdmin } from '#/auth.tsx'
import { usePolling } from '#/hooks/usePolling.ts'

const WorkspaceContext = createContext<{
  selected: string
  workspace: Workspace | null
  workspaces: Workspace[]
  team: Workspace | null
  teams: Workspace[]
  storageBudget: number
  choose: (id: string) => void
  refresh: () => void
}>({
  selected: '',
  workspace: null,
  workspaces: [],
  team: null,
  teams: [],
  storageBudget: 0,
  choose: () => {},
  refresh: () => {},
})

export const useTeam = () => useContext(WorkspaceContext)
export const useWorkspace = useTeam
export const TeamContext = WorkspaceContext

export function TeamGate({ children }: { children: ReactNode }) {
  const { user } = useAccount()
  const list = usePolling(teamsAPI.list, 15000)
  const key = `mist-workspace-v2-${user?.id ?? 'local'}`
  const [selected, setSelected] = useState(
    () => localStorage.getItem(key) ?? '',
  )
  useLayoutEffect(() => {
    setActiveTeam(selected)
  }, [selected])

  function choose(id: string) {
    setActiveTeam(id)
    localStorage.setItem(key, id)
    setSelected(id)
  }

  useEffect(() => {
    const revoke = (event: Event) => {
      if ((event as CustomEvent<string>).detail === selected) {
        setActiveTeam('')
        localStorage.setItem(key, '')
        setSelected('')
        list.refresh()
      }
    }
    window.addEventListener('mist:workspace-access-revoked', revoke)
    return () =>
      window.removeEventListener('mist:workspace-access-revoked', revoke)
  }, [selected, key, list.refresh])

  const workspaces = list.data?.workspaces ?? list.data?.teams ?? []
  const workspace = workspaces.find((t) => t.id === selected) ?? null

  return (
    <WorkspaceContext
      value={{
        selected,
        workspace,
        workspaces,
        team: workspace,
        teams: workspaces,
        storageBudget: list.data?.storage_budget_gib ?? 0,
        choose,
        refresh: list.refresh,
      }}
    >
      {list.error && (
        <p role="alert" className="px-6 py-2 text-red-700">
          Workspace list unavailable: {list.error}{' '}
          <button onClick={list.refresh}>Retry</button>
        </p>
      )}
      <div key={selected}>{children}</div>
    </WorkspaceContext>
  )
}

export function WorkspaceSelector() {
  const { user } = useAccount()
  const { selected, workspaces, choose } = useTeam()
  const admin = isUserAdmin(user)
  const available = workspaces.filter(
    (t) => !t.disabled && (admin || t.members.some((m) => m.id === user?.id)),
  )

  const personalWorkspaces = available.filter((w) => w.type === 'personal')
  const teamWorkspaces = available.filter((w) => !w.type || w.type === 'team')
  const otherWorkspaces = available.filter(
    (w) => w.type === 'collaboration' || w.type === 'admin',
  )

  return (
    <div>
      <label htmlFor="workspace" className="mr-2 text-xs font-semibold uppercase tracking-wider text-gray-500">
        Workspace
      </label>
      <Picker
        id="workspace"
        className="border rounded p-2 text-sm w-full"
        value={selected}
        onChange={(e) => choose(e.target.value)}
      >
        <option value="">Select workspace...</option>
        {personalWorkspaces.length > 0 && (
          <optgroup label="Personal Quotas">
            {personalWorkspaces.map((w) => (
              <option key={w.id} value={w.id}>
                👤 {w.name}
              </option>
            ))}
          </optgroup>
        )}
        {teamWorkspaces.length > 0 && (
          <optgroup label="Team Workspaces">
            {teamWorkspaces.map((w) => (
              <option key={w.id} value={w.id}>
                👥 {w.name}
              </option>
            ))}
          </optgroup>
        )}
        {otherWorkspaces.length > 0 && (
          <optgroup label="Collaborations & Infra">
            {otherWorkspaces.map((w) => (
              <option key={w.id} value={w.id}>
                ⚡ {w.name} {w.type === 'admin' ? '(Admin Allocation)' : ''}
              </option>
            ))}
          </optgroup>
        )}
        {selected && !available.some((t) => t.id === selected) && (
          <option value={selected}>Membership unavailable</option>
        )}
        <option value="">Unassigned (legacy)</option>
      </Picker>
    </div>
  )
}

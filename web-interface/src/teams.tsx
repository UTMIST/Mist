import { Picker } from '#/components/Picker.tsx'
import {
  createContext,
  useContext,
  useLayoutEffect,
  useState,
  useEffect,
} from 'react'
import type { ReactNode } from 'react'
import type { Team } from '#/api.ts'
import { setActiveTeam, teamsAPI } from '#/api.ts'
import { useAccount } from '#/auth.tsx'
import { usePolling } from '#/hooks/usePolling.ts'

const TeamContext = createContext<{
  selected: string
  team: Team | null
  teams: Team[]
  choose: (id: string) => void
  refresh: () => void
}>({ selected: '', team: null, teams: [], choose: () => {}, refresh: () => {} })
export const useTeam = () => useContext(TeamContext)

export function TeamGate({ children }: { children: ReactNode }) {
  const { user } = useAccount()
  const list = usePolling(teamsAPI.list, 15000)
  const key = `mist-workspace-v1-${user?.id ?? 'local'}`
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
  const teams = list.data?.teams ?? []
  const team = teams.find((t) => t.id === selected) ?? null
  return (
    <TeamContext
      value={{ selected, team, teams, choose, refresh: list.refresh }}
    >
      {list.error && (
        <p role="alert" className="px-6 py-2 text-red-700">
          Team list unavailable: {list.error}{' '}
          <button onClick={list.refresh}>Retry</button>
        </p>
      )}
      <div key={selected}>{children}</div>
    </TeamContext>
  )
}

export function WorkspaceSelector() {
  const { user } = useAccount()
  const { selected, teams, choose } = useTeam()
  const available = teams.filter(
    (t) => !t.disabled && t.members.some((m) => m.id === user?.id),
  )
  return (
    <div>
      <label htmlFor="workspace" className="mr-2">
        Workspace
      </label>
      <Picker
        id="workspace"
        className="border rounded p-2"
        value={selected}
        onChange={(e) => choose(e.target.value)}
      >
        <option value="">Legacy files and jobs</option>
        {selected && !available.some((t) => t.id === selected) && (
          <option value={selected}>Membership unavailable</option>
        )}
        {available.map((t) => (
          <option key={t.id} value={t.id}>
            {t.name}
          </option>
        ))}
      </Picker>
    </div>
  )
}

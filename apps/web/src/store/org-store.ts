import { create } from "zustand"
import { persist } from "zustand/middleware"
import type { Org, OrgRole } from "@/types"

interface OrgStore {
  currentOrg: Org | null
  orgs: Org[]
  currentRole: OrgRole | null
  setOrgs: (orgs: Org[]) => void
  setCurrentOrg: (org: Org) => void
  setCurrentRole: (role: OrgRole | null) => void
  reset: () => void
}

export const useOrgStore = create<OrgStore>()(
  persist(
    (set) => ({
      currentOrg: null,
      orgs: [],
      currentRole: null,
      // The role belongs to an organisation, so it is cleared only when the
      // current one changes. A rename or a refreshed list keeps the same id,
      // and the role query (keyed by that id) would not run again to fill it.
      setOrgs: (orgs) =>
        set((s) => {
          const currentOrg = orgs.find((o) => o.id === s.currentOrg?.id) ?? orgs[0] ?? null
          return {
            orgs,
            currentOrg,
            currentRole: currentOrg?.id === s.currentOrg?.id ? s.currentRole : null,
          }
        }),
      setCurrentOrg: (org) =>
        set((s) => ({ currentOrg: org, currentRole: org.id === s.currentOrg?.id ? s.currentRole : null })),
      setCurrentRole: (role) => set({ currentRole: role }),
      reset: () => set({ currentOrg: null, orgs: [], currentRole: null }),
    }),
    { name: "meshploy-org" }
  )
)

// true only when role is confirmed owner or admin - false while loading (null)
// so guards never flash-redirect before the fetch completes.
export function useIsAdmin(): boolean {
  const role = useOrgStore((s) => s.currentRole)
  return role === "owner" || role === "admin"
}

export function useOrgRole(): OrgRole | null {
  return useOrgStore((s) => s.currentRole)
}

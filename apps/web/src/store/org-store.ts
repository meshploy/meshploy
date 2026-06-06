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
      setOrgs: (orgs) =>
        set((s) => ({
          orgs,
          currentOrg:
            orgs.find((o) => o.id === s.currentOrg?.id) ?? orgs[0] ?? null,
          currentRole: null,
        })),
      setCurrentOrg: (org) => set({ currentOrg: org, currentRole: null }),
      setCurrentRole: (role) => set({ currentRole: role }),
      reset: () => set({ currentOrg: null, orgs: [], currentRole: null }),
    }),
    { name: "meshploy-org" }
  )
)

// true only when role is confirmed owner or admin — false while loading (null)
// so guards never flash-redirect before the fetch completes.
export function useIsAdmin(): boolean {
  const role = useOrgStore((s) => s.currentRole)
  return role === "owner" || role === "admin"
}

export function useOrgRole(): OrgRole | null {
  return useOrgStore((s) => s.currentRole)
}

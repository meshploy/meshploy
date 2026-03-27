import { create } from "zustand"
import { persist } from "zustand/middleware"
import type { Org } from "@/types"

interface OrgStore {
  currentOrg: Org | null
  orgs: Org[]
  setOrgs: (orgs: Org[]) => void
  setCurrentOrg: (org: Org) => void
  reset: () => void
}

export const useOrgStore = create<OrgStore>()(
  persist(
    (set) => ({
      currentOrg: null,
      orgs: [],
      setOrgs: (orgs) =>
        set((s) => ({
          orgs,
          // Keep current org if it's still in the list, otherwise pick first
          currentOrg:
            orgs.find((o) => o.id === s.currentOrg?.id) ?? orgs[0] ?? null,
        })),
      setCurrentOrg: (org) => set({ currentOrg: org }),
      reset: () => set({ currentOrg: null, orgs: [] }),
    }),
    { name: "meshploy-org" }
  )
)

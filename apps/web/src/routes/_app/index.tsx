import { createFileRoute, redirect } from "@tanstack/react-router"
import { domains as domainsApi } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"

export const Route = createFileRoute("/_app/")({
  beforeLoad: async () => {
    const { token } = useAuthStore.getState()
    const { currentOrg } = useOrgStore.getState()
    if (token && currentOrg) {
      try {
        const list = await domainsApi.list(currentOrg.id, token)
        if (list.length === 0) throw redirect({ to: "/domains/new" })
      } catch (e) {
        // If it's already a redirect, re-throw it
        if (e instanceof Error === false) throw e
      }
    }
    throw redirect({ to: "/nodes" })
  },
})

import { createFileRoute, Outlet, redirect } from "@tanstack/react-router"
import { AppSidebar } from "@/components/layout/app-sidebar"
import { Topbar } from "@/components/layout/topbar"
import { TabBar } from "@/components/layout/tab-bar"
import { DBExplorer } from "@/components/explorer/db-explorer"
import { NodeTerminal } from "@/components/terminal/node-terminal"
import { ServiceTerminal } from "@/components/terminal/service-terminal"
import { NodeMetricsTab } from "@/components/metrics/node-metrics-tab"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { useTabStore, type SessionTab, type ExplorerPayload, type TerminalPayload, type MetricsPayload, type ServiceTerminalPayload } from "@/store/tab-store"
import { cn } from "@/lib/utils"
import { useQuery } from "@tanstack/react-query"
import { orgs as orgsApi, auth } from "@/lib/api"

export const Route = createFileRoute("/_app")({
  beforeLoad: async () => {
    const { token } = useAuthStore.getState()
    if (!token) throw redirect({ to: "/login" })

    // A gateway with no domain cannot run most of the console: every template
    // needs a subdomain, routes need a hostname, and workers join through
    // headscale.<domain>. Gate once here rather than letting each feature fail
    // on its own — a console whose main paths error is worse than one that says
    // what is missing.
    //
    // Failure to reach the endpoint is deliberately not a gate. The console
    // should not become unreachable because a status call timed out; the
    // features that need a domain will report it themselves.
    try {
      const status = await auth.status()
      if (status.setup_required) throw redirect({ to: "/setup-required" })
    } catch (e) {
      if (e && typeof e === "object" && "to" in e) throw e
    }
  },
  component: AppLayout,
})

function AppLayout() {
  const { tabs, activeTabId } = useTabStore()
  const token = useAuthStore((s) => s.token)!
  const userId = useAuthStore((s) => s.userId)!
  const { currentOrg, setCurrentRole } = useOrgStore()

  // Keep currentRole in sync with the server. staleTime of 5 min — no need to
  // re-fetch on every navigation, but refreshes after an org switch (currentRole
  // is set to null by setCurrentOrg, which invalidates this query key).
  useQuery({
    queryKey: ["my-org-role", currentOrg?.id, userId],
    queryFn: async () => {
      const members = await orgsApi.listMembers(currentOrg!.id, token)
      const me = members.find((m) => m.user_id === userId)
      const role = (me?.role ?? "member") as "owner" | "admin" | "member"
      setCurrentRole(role)
      return role
    },
    enabled: !!currentOrg && !!token && !!userId,
    staleTime: 5 * 60 * 1000,
  })

  return (
    <div className="flex h-full">
      <AppSidebar />
      <div className="flex flex-col flex-1 min-w-0 overflow-hidden">
        <Topbar />
        <TabBar />
        <main className="flex-1 overflow-hidden flex flex-col min-h-0">
          <div className={cn("flex-1 overflow-y-auto", activeTabId !== null && "hidden")}>
            <Outlet />
          </div>
          {tabs.map((tab) => (
            <div key={tab.id} className={cn("flex-1 overflow-hidden", activeTabId !== tab.id && "hidden")}>
              <SessionContent tab={tab} />
            </div>
          ))}
        </main>
      </div>
    </div>
  )
}

function SessionContent({ tab }: { tab: SessionTab }) {
  if (tab.type === "explorer") {
    const payload = tab.payload as ExplorerPayload
    return <DBExplorer projectId={payload.projectId} serviceId={payload.serviceId} />
  }
  if (tab.type === "terminal") {
    const payload = tab.payload as TerminalPayload
    return <NodeTerminal payload={payload} />
  }
  if (tab.type === "metrics") {
    const payload = tab.payload as MetricsPayload
    return <NodeMetricsTab payload={payload} />
  }
  if (tab.type === "service-terminal") {
    const payload = tab.payload as ServiceTerminalPayload
    return <ServiceTerminal payload={payload} />
  }
  return null
}


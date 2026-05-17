import { createFileRoute, Outlet, redirect } from "@tanstack/react-router"
import { AppSidebar } from "@/components/layout/app-sidebar"
import { Topbar } from "@/components/layout/topbar"
import { TabBar } from "@/components/layout/tab-bar"
import { DBExplorer } from "@/components/explorer/db-explorer"
import { NodeTerminal } from "@/components/terminal/node-terminal"
import { NodeMetricsTab } from "@/components/metrics/node-metrics-tab"
import { useAuthStore } from "@/store/auth-store"
import { useTabStore, type SessionTab, type ExplorerPayload, type TerminalPayload, type MetricsPayload } from "@/store/tab-store"
import { cn } from "@/lib/utils"

export const Route = createFileRoute("/_app")({
  beforeLoad: () => {
    const { token } = useAuthStore.getState()
    if (!token) throw redirect({ to: "/login" })
  },
  component: AppLayout,
})

function AppLayout() {
  const { tabs, activeTabId } = useTabStore()

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
  return null
}


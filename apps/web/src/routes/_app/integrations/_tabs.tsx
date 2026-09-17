import { createFileRoute, Link, Outlet, useRouterState } from "@tanstack/react-router"
import { useQueries } from "@tanstack/react-query"
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"
import {
  gitIntegrations as gitApi,
  registries as registriesApi,
  storage as storageApi,
  notifications as notificationsApi,
} from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"

export const Route = createFileRoute("/_app/integrations/_tabs")({
  component: IntegrationsLayout,
})

const TABS = [
  { id: "git", label: "Git sources", to: "/integrations/git" },
  { id: "registries", label: "Registries", to: "/integrations/registries" },
  { id: "storage", label: "Object storage", to: "/integrations/storage" },
  { id: "notifications", label: "Notifications", to: "/integrations/notifications" },
  { id: "email", label: "Email provider", to: "/integrations/email" },
] as const

function IntegrationsLayout() {
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)!
  const pathname = useRouterState({ select: (s) => s.location.pathname })
  const active = TABS.find((t) => pathname.startsWith(t.to))?.id ?? "git"

  const counts = useQueries({ queries: [
    { queryKey: ["git-integrations", orgId], queryFn: () => gitApi.list(orgId, token).then((r) => r ?? []), enabled: !!orgId },
    { queryKey: ["registry-integrations", orgId], queryFn: () => registriesApi.list(orgId, token), enabled: !!orgId },
    { queryKey: ["storage-integrations", orgId], queryFn: () => storageApi.list(orgId, token), enabled: !!orgId },
    { queryKey: ["notification-channels", orgId], queryFn: () => notificationsApi.list(orgId, token), enabled: !!orgId },
  ] }).map((q) => q.isPending ? "…" : q.isError ? "-" : (q.data?.length ?? 0))

  return (
    <div className="console-page p-6 space-y-8">
      <div>
        <h1 className="text-xl font-semibold tracking-tight">Integrations</h1>
        <p className="text-sm text-muted-foreground mt-0.5">
          Connect external services for source code, images, backups, and alerts
        </p>
      </div>

      <Tabs value={active} className="gap-6 min-w-0">
        <div className="overflow-x-auto border-b border-border">
          <TabsList variant="line" aria-label="Integration categories" className="detail-tabs console-panel-tabs">
            {TABS.map((tab, index) => (
              <TabsTrigger key={tab.id} value={tab.id} className="console-detail-tab" render={<Link to={tab.to} />} nativeButton={false}>
                {tab.label}
                {tab.id !== "email" && <span className="text-xs text-muted-foreground tabular-nums">{counts[index]}</span>}
              </TabsTrigger>
            ))}
          </TabsList>
        </div>
        <Outlet />
      </Tabs>
    </div>
  )
}

import { createFileRoute, useNavigate } from "@tanstack/react-router"
import { useEffect } from "react"
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import {
  Cpu,
  HardDrive,
  MemoryStick,
  Network,
  Copy,
  Eye,
  EyeOff,
  RefreshCw,
  Terminal,
  Check,
  Loader2,
  Plus,
  ShieldAlert,
} from "lucide-react"
import { useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle,
} from "@/components/ui/dialog"
import { Switch } from "@/components/ui/switch"
import { nodes as nodesApi, cluster as clusterApi, toNode, ApiError } from "@/lib/api"
import type { MeshHealth, OrphanWorkload } from "@/lib/api/cluster"
import { formatRelativeTime } from "@/lib/utils"
import { MeshGraph } from "@/routes/_app/index"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore, useOrgRole } from "@/store/org-store"

export const Route = createFileRoute("/_app/cluster/")({
  component: ClusterPage,
})

// MeshHealthBanner warns when the control plane cannot reach Headscale. Without
// it the failure is invisible: node liveness simply freezes at its last known
// value and every screen keeps presenting that stale value as current.
function MeshHealthBanner({ health }: { health?: MeshHealth }) {
  if (!health || !health.configured || !health.checked || health.healthy) return null

  const lastOK = health.last_success_at ? new Date(health.last_success_at) : null
  return (
    <div className="flex items-start gap-2.5 rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2.5">
      <ShieldAlert className="h-4 w-4 text-destructive shrink-0 mt-0.5" />
      <div className="space-y-1 min-w-0">
        <p className="text-xs font-medium text-destructive">
          {health.unauthorized
            ? "Headscale rejected the control plane's API key"
            : "Control plane cannot reach Headscale"}
        </p>
        <p className="text-xs text-muted-foreground">
          Mesh status below is stale{lastOK ? ` — last confirmed ${formatRelativeTime(lastOK)}` : ""}.
          Node online/offline changes are not being detected.
          {health.unauthorized && (
            <>
              {" "}Mint a new key and set <code className="text-[11px]">HEADSCALE_API_KEY</code>:{" "}
              <code className="text-[11px]">headscale apikeys create -e 3650d</code>
            </>
          )}
        </p>
        {health.last_error && (
          <p className="text-[11px] text-muted-foreground/70 font-mono truncate">{health.last_error}</p>
        )}
      </div>
    </div>
  )
}

function ClusterPage() {
  const role = useOrgRole()
  const navigate = useNavigate()
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)

  useEffect(() => {
    if (role === "member") navigate({ to: "/" })
  }, [role])

  const { data: rawNodes = [] } = useQuery({
    queryKey: ["nodes", orgId],
    queryFn: () => nodesApi.list(orgId!, token),
    enabled: !!orgId,
  })

  const { data: orphanData } = useQuery({
    queryKey: ["cluster-orphans", orgId],
    queryFn: () => clusterApi.listOrphans(orgId!, token),
    enabled: !!orgId,
    refetchInterval: 60_000,
  })
  const orphans = orphanData?.orphans ?? []
  const qc = useQueryClient()
  const [removeTarget, setRemoveTarget] = useState<OrphanWorkload | null>(null)
  const [removeData, setRemoveData] = useState(false)

  const removeOrphan = useMutation({
    mutationFn: () =>
      clusterApi.deleteOrphan(orgId!, removeTarget!.namespace, removeTarget!.name, removeData, token),
    onSuccess: () => {
      setRemoveTarget(null)
      qc.invalidateQueries({ queryKey: ["cluster-orphans", orgId] })
    },
  })

  const { data: meshHealth } = useQuery({
    queryKey: ["mesh-health", orgId],
    queryFn: () => clusterApi.getMeshHealth(orgId!, token),
    enabled: !!orgId,
    refetchInterval: 60_000,
  })

  // Only show nodes that are in the k8s cluster
  const clusterNodes = rawNodes.map(toNode).filter((n) => n.k8sMember)
  const online = clusterNodes.filter((n) => n.status === "online")
  const servers = clusterNodes.filter((n) => n.k3sRole === "server")
  const agents = clusterNodes.filter((n) => n.k3sRole === "agent")

  const totalCPU = clusterNodes.reduce((s, n) => s + n.cpuCores, 0)
  const totalMemGB = clusterNodes.reduce((s, n) => s + n.memoryGB, 0)
  const totalDiskGB = clusterNodes.reduce((s, n) => s + n.diskGB, 0)
  const onlineCPU = online.reduce((s, n) => s + n.cpuCores, 0)
  const onlineMemGB = online.reduce((s, n) => s + n.memoryGB, 0)

  const versionMap = new Map<string, number>()
  clusterNodes.forEach((n) => {
    if (n.k3sVersion) versionMap.set(n.k3sVersion, (versionMap.get(n.k3sVersion) ?? 0) + 1)
  })

  // Find latest version to mark others outdated
  const versions = Array.from(versionMap.keys()).sort().reverse()
  const latestVersion = versions[0] ?? ""

  return (
    <div className="p-6 space-y-6">
      <div>
        <h1 className="text-xl font-semibold tracking-tight">Cluster</h1>
        <p className="text-sm text-muted-foreground mt-0.5">Single K3s cluster spanning all mesh nodes</p>
      </div>

      <MeshHealthBanner health={meshHealth} />

      <Dialog open={Boolean(removeTarget)} onOpenChange={(o) => !o && setRemoveTarget(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Remove this workload?</DialogTitle>
            <DialogDescription>
              <span className="font-mono text-foreground">
                {removeTarget?.namespace}/{removeTarget?.name}
              </span>{" "}
              is running in the cluster with no service behind it. Removing it deletes the
              deployment and its services. It is checked again first — if a service has since
              claimed this name, nothing is removed.
            </DialogDescription>
          </DialogHeader>

          {removeTarget?.has_pvc && (
            <div className="flex items-start justify-between gap-4 rounded-lg border border-destructive/30 bg-destructive/5 p-3">
              <div className="space-y-0.5">
                <p className="text-xs font-medium text-destructive">Also delete its data</p>
                <p className="text-[11px] text-muted-foreground/70">
                  This workload has a volume claim. Left alone the data survives and can be
                  reattached; deleted, it is gone. Removing the workload does not touch it unless
                  you say so here.
                </p>
              </div>
              <Switch checked={removeData} onCheckedChange={(v) => setRemoveData(Boolean(v))} />
            </div>
          )}

          {removeOrphan.isError && (
            <p className="text-xs text-destructive">{(removeOrphan.error as Error).message}</p>
          )}

          <DialogFooter>
            <Button variant="outline" size="sm" onClick={() => setRemoveTarget(null)}>
              Cancel
            </Button>
            <Button
              variant="destructive"
              size="sm"
              className="gap-1.5"
              onClick={() => removeOrphan.mutate()}
              disabled={removeOrphan.isPending}
            >
              {removeOrphan.isPending && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
              Remove
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {orphans.length > 0 && (
        <div className="rounded-lg border border-amber-500/30 bg-amber-500/5 overflow-hidden">
          <div className="px-4 py-3 border-b border-amber-500/20 flex items-start gap-2.5">
            <ShieldAlert className="h-4 w-4 text-amber-400 shrink-0 mt-0.5" />
            <div>
              <p className="text-xs font-medium text-amber-400">
                {orphans.length} workload{orphans.length === 1 ? "" : "s"} with no service behind{" "}
                {orphans.length === 1 ? "it" : "them"}
              </p>
              <p className="text-[11px] text-muted-foreground/70 mt-0.5">
                Running in the cluster but absent from meshploy — left by a delete that could not
                reach the cluster, a rename from before renames moved the workload, or a change made
                with kubectl. They still hold memory on their node. Remove one once you have
                confirmed what it is.
              </p>
            </div>
          </div>
          <div className="divide-y divide-amber-500/10">
            {orphans.map((o) => (
              <div key={`${o.namespace}/${o.name}`} className="px-4 py-2.5 flex items-center gap-3">
                <code className="text-xs font-mono text-foreground flex-1 min-w-0 truncate">
                  {o.namespace}/{o.name}
                </code>
                {o.has_pvc && (
                  <Badge variant="secondary" className="text-[10px] px-1.5 py-0 h-4 shrink-0">
                    has data
                  </Badge>
                )}
                <span className="text-[11px] text-muted-foreground/60 shrink-0">
                  {o.ready}/{o.replicas} ready · {o.age_days}d old
                </span>
                <Button
                  size="sm"
                  variant="ghost"
                  className="h-6 px-2 text-[11px] text-muted-foreground hover:text-destructive shrink-0"
                  onClick={() => { setRemoveTarget(o); setRemoveData(false) }}
                >
                  Remove
                </Button>
              </div>
            ))}
          </div>
        </div>
      )}

      <div className="grid gap-3 grid-cols-2 lg:grid-cols-4">
        <StatCard
          icon={<Network className="h-4 w-4" />}
          label="Nodes"
          value={clusterNodes.length === 0 ? "0" : `${online.length}/${clusterNodes.length}`}
          sub="online"
          accent={clusterNodes.length > 0 && online.length < clusterNodes.length ? "warn" : undefined}
        />
        <StatCard icon={<Cpu className="h-4 w-4" />} label="CPU cores" value={String(onlineCPU || "—")} sub={totalCPU ? `${totalCPU} total` : "no nodes"} />
        <StatCard icon={<MemoryStick className="h-4 w-4" />} label="Memory" value={onlineMemGB ? `${onlineMemGB.toFixed(1)} GB` : "—"} sub={totalMemGB ? `${totalMemGB.toFixed(1)} GB total` : "no nodes"} />
        <StatCard icon={<HardDrive className="h-4 w-4" />} label="Disk" value={totalDiskGB ? `${totalDiskGB.toFixed(0)} GB` : "—"} sub={clusterNodes.length ? `${clusterNodes.length} nodes` : "no nodes"} />
      </div>

      <div className="grid gap-6 lg:grid-cols-2">
        {/* Mesh topology visualization */}
        <div className="rounded-lg border border-border/60 overflow-hidden">
          <div className="px-4 py-3 border-b border-border/40 bg-muted/20 flex items-center justify-between">
            <p className="text-xs font-medium text-muted-foreground uppercase tracking-wider">Mesh topology</p>
            <Badge variant="secondary" className="text-[10px] px-1.5 py-0 h-4.5">WireGuard</Badge>
          </div>
          <div className="p-4">
            <MeshGraph nodes={clusterNodes} height={260} />
          </div>
        </div>

        <div className="space-y-4">
          {/* Role breakdown */}
          <div className="rounded-lg border border-border/60 overflow-hidden">
            <div className="px-4 py-3 border-b border-border/40 bg-muted/20">
              <p className="text-xs font-medium text-muted-foreground uppercase tracking-wider">Role breakdown</p>
            </div>
            <div className="p-4 space-y-3">
              <RoleBar label="Control plane" count={servers.length} total={clusterNodes.length} color="bg-primary" />
              <RoleBar label="Worker agents" count={agents.length} total={clusterNodes.length} color="bg-primary/40" />
            </div>
          </div>

          {/* K3s versions */}
          {versionMap.size > 0 && (
            <div className="rounded-lg border border-border/60 overflow-hidden">
              <div className="px-4 py-3 border-b border-border/40 bg-muted/20">
                <p className="text-xs font-medium text-muted-foreground uppercase tracking-wider">K3s versions</p>
              </div>
              <div className="divide-y divide-border/30">
                {Array.from(versionMap.entries()).map(([ver, count]) => (
                  <div key={ver} className="flex items-center justify-between px-4 py-3">
                    <code className="text-xs font-mono text-foreground">{ver}</code>
                    <div className="flex items-center gap-2">
                      <span className="text-xs text-muted-foreground">{count} {count === 1 ? "node" : "nodes"}</span>
                      {ver !== latestVersion && (
                        <Badge variant="secondary" className="text-[10px] px-1.5 py-0 h-4.5">outdated</Badge>
                      )}
                    </div>
                  </div>
                ))}
              </div>
            </div>
          )}
        </div>
      </div>

      {/* Tokens row */}
      <div className="grid gap-4 lg:grid-cols-3">
        <ProvisioningTokensPanel />
        <HeadscalePreAuthKeyPanel />
        <K3sJoinTokenPanel />
      </div>
    </div>
  )
}

// ─── Provisioning tokens panel ────────────────────────────────────────────────

const MESH_API_URL = "http://100.64.0.1:4000"

function ProvisioningTokensPanel() {
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)
  const [provToken, setProvToken] = useState("")
  const [visible, setVisible] = useState(false)
  const [copiedField, setCopiedField] = useState<string | null>(null)

  const [error, setError] = useState<string | null>(null)

  const { mutate: generate, isPending: generating } = useMutation({
    mutationFn: () => nodesApi.createProvisioningToken(orgId!, "worker", null, token),
    onSuccess: (res) => {
      setProvToken(res.token)
      setVisible(true)
      setError(null)
    },
    // Without this a failure is silent: the spinner stops and no token appears,
    // which reads as the button doing nothing.
    onError: (e) =>
      setError(e instanceof ApiError ? e.detail : "Could not generate a token."),
  })

  const copy = async (text: string, field: string) => {
    await navigator.clipboard.writeText(text)
    setCopiedField(field)
    setTimeout(() => setCopiedField(null), 2000)
  }

  const curlCommand = provToken
    ? `curl -fsSL https://raw.githubusercontent.com/meshploy/meshploy/main/deploy/install.sh | \\\n  MESHPLOY_API_URL="${MESH_API_URL}" MESHPLOY_TOKEN="${provToken}" bash`
    : ""

  return (
    <div className="rounded-lg border border-border/60 overflow-hidden">
      <div className="px-4 py-3 border-b border-border/40 bg-muted/20 flex items-center justify-between">
        <p className="text-xs font-medium text-muted-foreground uppercase tracking-wider">Add a worker node</p>
        <Button
          size="sm"
          variant="outline"
          className="h-7 text-xs gap-1.5"
          onClick={() => generate()}
          // !orgId is required: this panel has no query to gate on, so nothing
          // else stops a click during the window before the org store hydrates
          // — which would POST to /orgs/undefined/provisioning-tokens.
          disabled={generating || !orgId}
        >
          {generating ? <Loader2 className="h-3 w-3 animate-spin" /> : <RefreshCw className="h-3 w-3" />}
          {provToken ? "New token" : "Generate token"}
        </Button>
      </div>

      <div className="p-4 space-y-4">
        {error && (
          <div className="flex items-start gap-2 rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2">
            <ShieldAlert className="h-3.5 w-3.5 text-destructive shrink-0 mt-0.5" />
            <p className="text-xs text-destructive">{error}</p>
          </div>
        )}
        {!provToken ? (
          <p className="text-sm text-muted-foreground">
            Generate a single-use token to get the worker install command.
          </p>
        ) : (
          <>
            {/* One-time warning */}
            <div className="flex items-start gap-2 rounded-md border border-amber-500/20 bg-amber-500/5 px-3 py-2">
              <ShieldAlert className="h-3.5 w-3.5 text-amber-400 shrink-0 mt-0.5" />
              <p className="text-xs text-amber-300/90">
                Shown once — copy before leaving. Auto-invalidates after the node registers.
              </p>
            </div>

            {/* Token display */}
            <div className="space-y-1.5">
              <p className="text-xs text-muted-foreground font-medium">Provisioning token</p>
              <div className="flex items-center gap-2">
                <code className="flex-1 text-xs font-mono bg-muted/50 border border-border/40 rounded px-3 py-2 text-foreground overflow-hidden text-ellipsis whitespace-nowrap">
                  {visible ? provToken : "mprov-" + "•".repeat(64)}
                </code>
                <Button size="icon" variant="ghost" className="h-8 w-8 shrink-0" onClick={() => setVisible((v) => !v)}>
                  {visible ? <EyeOff className="h-3.5 w-3.5" /> : <Eye className="h-3.5 w-3.5" />}
                </Button>
                <Button size="icon" variant="ghost" className="h-8 w-8 shrink-0" onClick={() => copy(provToken, "token")}>
                  {copiedField === "token" ? <Check className="h-3.5 w-3.5 text-emerald-400" /> : <Copy className="h-3.5 w-3.5" />}
                </Button>
              </div>
            </div>

            {/* Install command */}
            <div className="space-y-1.5">
              <p className="text-xs text-muted-foreground font-medium">Run on the worker machine</p>
              <div className="relative group">
                <div className="flex items-start gap-2 bg-muted/30 border border-border/40 rounded px-3 py-2.5">
                  <Terminal className="h-3.5 w-3.5 text-muted-foreground shrink-0 mt-0.5" />
                  <code className="text-xs font-mono text-foreground whitespace-pre-wrap break-all leading-relaxed">
                    {curlCommand}
                  </code>
                </div>
                <Button
                  size="icon"
                  variant="ghost"
                  className="absolute top-1.5 right-1.5 h-7 w-7 opacity-0 group-hover:opacity-100 transition-opacity"
                  onClick={() => copy(curlCommand.replace(/\\\n  /g, " "), "cmd")}
                >
                  {copiedField === "cmd" ? <Check className="h-3 w-3 text-emerald-400" /> : <Copy className="h-3 w-3" />}
                </Button>
              </div>
              <p className="text-[11px] text-muted-foreground/60">
                Connects over the WireGuard mesh ({MESH_API_URL}) — no public internet needed after joining Headscale.
              </p>
            </div>
          </>
        )}
      </div>
    </div>
  )
}

// ─── Headscale preauth key panel ─────────────────────────────────────────────

function HeadscalePreAuthKeyPanel() {
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)
  const queryClient = useQueryClient()
  const [visible, setVisible] = useState(false)
  const [copiedField, setCopiedField] = useState<string | null>(null)

  const { data, isLoading } = useQuery({
    queryKey: ["headscale-preauth-key", orgId],
    queryFn: () => clusterApi.getHeadscalePreAuthKey(orgId!, token),
    enabled: !!orgId,
  })

  const { mutate: generate, isPending: generating } = useMutation({
    mutationFn: () => clusterApi.createHeadscalePreAuthKey(orgId!, token),
    onSuccess: (res) => {
      setVisible(true)
      queryClient.setQueryData(["headscale-preauth-key", orgId], {
        has_active_key: true,
        key: res.key,
        headscale_url: res.headscale_url,
      })
    },
  })

  const copy = async (text: string, field: string) => {
    await navigator.clipboard.writeText(text)
    setCopiedField(field)
    setTimeout(() => setCopiedField(null), 2000)
  }

  const headscaleUrl = data?.headscale_url || ""
  // activeKey: populated from stored key (GET) or freshly generated key (POST via setQueryData)
  const activeKey = data?.key || ""

  // Headscale not configured — GET returns empty headscale_url
  const unavailable = !isLoading && !headscaleUrl

  const tailscaleCmd = activeKey
    ? `tailscale up \\\n  --login-server="${headscaleUrl}" \\\n  --authkey="${activeKey}" \\\n  --force-reauth`
    : ""

  return (
    <div className="rounded-lg border border-border/60 overflow-hidden">
      <div className="px-4 py-3 border-b border-border/40 bg-muted/20 flex items-center justify-between">
        <p className="text-xs font-medium text-muted-foreground uppercase tracking-wider">Headscale preauth key</p>
        <Button
          size="sm"
          variant="outline"
          className="h-7 text-xs gap-1.5"
          onClick={() => generate()}
          // !orgId is required: the query above is disabled (not loading) until
          // the org resolves, so isLoading is false and the button would
          // otherwise POST to /orgs/undefined/...
          disabled={generating || isLoading || unavailable || !orgId}
        >
          {generating ? <Loader2 className="h-3 w-3 animate-spin" /> : <RefreshCw className="h-3 w-3" />}
          {activeKey ? "New key" : "Generate key"}
        </Button>
      </div>

      <div className="p-4 space-y-4">
        {isLoading ? (
          <div className="flex items-center gap-2 text-muted-foreground text-sm">
            <Loader2 className="h-3.5 w-3.5 animate-spin" />
            <span>Loading…</span>
          </div>
        ) : unavailable ? (
          <p className="text-sm text-muted-foreground">Headscale is not configured on this gateway.</p>
        ) : (
          <>
            {/* Headscale server URL — always visible so users can copy it during worker install */}
            <div className="space-y-1.5">
              <p className="text-xs text-muted-foreground font-medium">Headscale server URL</p>
              <div className="flex items-center gap-2">
                <code className="flex-1 text-xs font-mono bg-muted/50 border border-border/40 rounded px-3 py-2 text-foreground overflow-hidden text-ellipsis whitespace-nowrap">
                  {headscaleUrl}
                </code>
                <Button size="icon" variant="ghost" className="h-8 w-8 shrink-0" onClick={() => copy(headscaleUrl, "url")}>
                  {copiedField === "url" ? <Check className="h-3.5 w-3.5 text-emerald-400" /> : <Copy className="h-3.5 w-3.5" />}
                </Button>
              </div>
            </div>

            {activeKey ? (
              <>
                {/* Key display — shown whenever a valid stored key exists (survives page navigation) */}
                <div className="space-y-1.5">
                  <p className="text-xs text-muted-foreground font-medium">Preauth key</p>
                  <div className="flex items-center gap-2">
                    <code className="flex-1 text-xs font-mono bg-muted/50 border border-border/40 rounded px-3 py-2 text-foreground overflow-hidden text-ellipsis whitespace-nowrap">
                      {visible ? activeKey : "•".repeat(32)}
                    </code>
                    <Button size="icon" variant="ghost" className="h-8 w-8 shrink-0" onClick={() => setVisible((v) => !v)}>
                      {visible ? <EyeOff className="h-3.5 w-3.5" /> : <Eye className="h-3.5 w-3.5" />}
                    </Button>
                    <Button size="icon" variant="ghost" className="h-8 w-8 shrink-0" onClick={() => copy(activeKey, "key")}>
                      {copiedField === "key" ? <Check className="h-3.5 w-3.5 text-emerald-400" /> : <Copy className="h-3.5 w-3.5" />}
                    </Button>
                  </div>
                </div>

                {/* tailscale up command */}
                <div className="space-y-1.5">
                  <p className="text-xs text-muted-foreground font-medium">Run on the worker machine</p>
                  <div className="relative group">
                    <div className="flex items-start gap-2 bg-muted/30 border border-border/40 rounded px-3 py-2.5">
                      <Terminal className="h-3.5 w-3.5 text-muted-foreground shrink-0 mt-0.5" />
                      <code className="text-xs font-mono text-foreground whitespace-pre-wrap break-all leading-relaxed">
                        {tailscaleCmd}
                      </code>
                    </div>
                    <Button
                      size="icon"
                      variant="ghost"
                      className="absolute top-1.5 right-1.5 h-7 w-7 opacity-0 group-hover:opacity-100 transition-opacity"
                      onClick={() => copy(tailscaleCmd.replace(/\\\n  /g, " "), "cmd")}
                    >
                      {copiedField === "cmd" ? <Check className="h-3 w-3 text-emerald-400" /> : <Copy className="h-3 w-3" />}
                    </Button>
                  </div>
                  <p className="text-[11px] text-muted-foreground/60">
                    Key is reusable and valid for 1 year. Click "New key" to rotate.
                  </p>
                </div>
              </>
            ) : (
              <p className="text-sm text-muted-foreground">
                Click <span className="text-foreground font-medium">Generate key</span> to create a reusable preauth key for worker installation.
              </p>
            )}
          </>
        )}
      </div>
    </div>
  )
}

// ─── K3s join token panel ─────────────────────────────────────────────────────

function K3sJoinTokenPanel() {
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)
  const [visible, setVisible] = useState(false)
  const [copiedField, setCopiedField] = useState<string | null>(null)

  const { data, isLoading } = useQuery({
    queryKey: ["cluster-join-token", orgId],
    queryFn: () => clusterApi.getJoinToken(orgId!, token),
    enabled: !!orgId,
  })

  const k3sToken = data?.token ?? ""
  const serverUrl = data?.server_url ?? "https://100.64.0.1:6443"

  const copy = async (text: string, field: string) => {
    await navigator.clipboard.writeText(text)
    setCopiedField(field)
    setTimeout(() => setCopiedField(null), 2000)
  }

  const installCmd = k3sToken
    ? `curl -sfL https://get.k3s.io | \\\n  K3S_URL="${serverUrl}" \\\n  K3S_TOKEN="${k3sToken}" \\\n  sh -s - agent \\\n    --node-ip="$(tailscale ip -4)"`
    : ""

  return (
    <div className="rounded-lg border border-border/60 overflow-hidden">
      <div className="px-4 py-3 border-b border-border/40 bg-muted/20">
        <p className="text-xs font-medium text-muted-foreground uppercase tracking-wider">K3s cluster join token</p>
      </div>

      <div className="p-4 space-y-4">
        {isLoading ? (
          <div className="flex items-center gap-2 text-muted-foreground text-sm">
            <Loader2 className="h-3.5 w-3.5 animate-spin" />
            <span>Loading…</span>
          </div>
        ) : !k3sToken ? (
          <p className="text-sm text-muted-foreground">
            K3s is not installed on the gateway yet. Run the master install script to set up the cluster control plane.
          </p>
        ) : (
          <>
            {/* Token display */}
            <div className="space-y-1.5">
              <p className="text-xs text-muted-foreground font-medium">Node token</p>
              <div className="flex items-center gap-2">
                <code className="flex-1 text-xs font-mono bg-muted/50 border border-border/40 rounded px-3 py-2 text-foreground overflow-hidden text-ellipsis whitespace-nowrap">
                  {visible ? k3sToken : "K1" + "•".repeat(62)}
                </code>
                <Button size="icon" variant="ghost" className="h-8 w-8 shrink-0" onClick={() => setVisible((v) => !v)}>
                  {visible ? <EyeOff className="h-3.5 w-3.5" /> : <Eye className="h-3.5 w-3.5" />}
                </Button>
                <Button size="icon" variant="ghost" className="h-8 w-8 shrink-0" onClick={() => copy(k3sToken, "token")}>
                  {copiedField === "token" ? <Check className="h-3.5 w-3.5 text-emerald-400" /> : <Copy className="h-3.5 w-3.5" />}
                </Button>
              </div>
            </div>

            {/* Install command */}
            <div className="space-y-1.5">
              <p className="text-xs text-muted-foreground font-medium">Join an existing node to the cluster</p>
              <div className="relative group">
                <div className="flex items-start gap-2 bg-muted/30 border border-border/40 rounded px-3 py-2.5">
                  <Terminal className="h-3.5 w-3.5 text-muted-foreground shrink-0 mt-0.5" />
                  <code className="text-xs font-mono text-foreground whitespace-pre-wrap break-all leading-relaxed">
                    {installCmd}
                  </code>
                </div>
                <Button
                  size="icon"
                  variant="ghost"
                  className="absolute top-1.5 right-1.5 h-7 w-7 opacity-0 group-hover:opacity-100 transition-opacity"
                  onClick={() => copy(installCmd.replace(/\\\n  /g, " "), "cmd")}
                >
                  {copiedField === "cmd" ? <Check className="h-3 w-3 text-emerald-400" /> : <Copy className="h-3 w-3" />}
                </Button>
              </div>
              <p className="text-[11px] text-muted-foreground/60">
                Run on any mesh node. Requires the node to be on the WireGuard network first.
              </p>
            </div>
          </>
        )}
      </div>
    </div>
  )
}

// ─── Shared sub-components ────────────────────────────────────────────────────

function StatCard({ icon, label, value, sub, accent }: {
  icon: React.ReactNode
  label: string
  value: string
  sub: string
  accent?: "warn"
}) {
  return (
    <div className="rounded-lg border border-border/60 bg-card p-4 space-y-2">
      <div className="flex items-center gap-2 text-muted-foreground">{icon}<span className="text-xs font-medium">{label}</span></div>
      <p className={`text-2xl font-semibold tabular-nums ${accent === "warn" ? "text-amber-400" : "text-foreground"}`}>{value}</p>
      <p className="text-xs text-muted-foreground">{sub}</p>
    </div>
  )
}

function RoleBar({ label, count, total, color }: { label: string; count: number; total: number; color: string }) {
  const pct = total > 0 ? Math.round((count / total) * 100) : 0
  return (
    <div className="space-y-1.5">
      <div className="flex items-center justify-between text-xs">
        <span className="text-muted-foreground">{label}</span>
        <span className="text-foreground font-medium tabular-nums">
          {count} <span className="text-muted-foreground font-normal">/ {total}</span>
        </span>
      </div>
      <div className="h-1.5 rounded-full bg-muted overflow-hidden">
        <div className={`h-full rounded-full ${color}`} style={{ width: `${pct}%` }} />
      </div>
    </div>
  )
}

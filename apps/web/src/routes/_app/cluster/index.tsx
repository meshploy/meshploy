import { createFileRoute } from "@tanstack/react-router"
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
} from "lucide-react"
import { useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { NodeStatusDot } from "@/components/nodes/node-status-dot"
import { nodes as nodesApi, cluster as clusterApi, toNode, ApiError } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"

export const Route = createFileRoute("/_app/cluster/")({
  component: ClusterPage,
})

function ClusterPage() {
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)

  const { data: rawNodes = [] } = useQuery({
    queryKey: ["nodes", orgId],
    queryFn: () => nodesApi.list(orgId!, token),
    enabled: !!orgId,
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

      <div className="grid gap-3 grid-cols-2 lg:grid-cols-4">
        <StatCard
          icon={<Network className="h-4 w-4" />}
          label="Nodes"
          value={clusterNodes.length === 0 ? "0" : `${online.length}/${clusterNodes.length}`}
          sub="online"
          accent={clusterNodes.length > 0 && online.length < clusterNodes.length ? "warn" : undefined}
        />
        <StatCard icon={<Cpu className="h-4 w-4" />} label="CPU cores" value={String(onlineCPU || "—")} sub={totalCPU ? `${totalCPU} total` : "no nodes"} />
        <StatCard icon={<MemoryStick className="h-4 w-4" />} label="Memory" value={onlineMemGB ? `${onlineMemGB} GB` : "—"} sub={totalMemGB ? `${totalMemGB} GB total` : "no nodes"} />
        <StatCard icon={<HardDrive className="h-4 w-4" />} label="Disk" value={totalDiskGB ? `${totalDiskGB} GB` : "—"} sub={clusterNodes.length ? `${clusterNodes.length} nodes` : "no nodes"} />
      </div>

      <div className="grid gap-6 lg:grid-cols-2">
        {/* Node topology */}
        <div className="rounded-lg border border-border/60 overflow-hidden">
          <div className="px-4 py-3 border-b border-border/40 bg-muted/20">
            <p className="text-xs font-medium text-muted-foreground uppercase tracking-wider">Node topology</p>
          </div>
          {clusterNodes.length === 0 ? (
            <div className="px-4 py-8 text-center text-sm text-muted-foreground">
              No nodes in the cluster yet.
            </div>
          ) : (
            <div className="divide-y divide-border/30">
              {[...servers, ...agents].map((node) => (
                <div key={node.id} className="flex items-center gap-3 px-4 py-3">
                  <NodeStatusDot status={node.status} />
                  <div className="flex-1 min-w-0">
                    <p className="text-sm font-medium text-foreground truncate">{node.name}</p>
                    <code className="text-[11px] font-mono text-muted-foreground">{node.tailscaleIP}</code>
                  </div>
                  <div className="flex items-center gap-2 shrink-0">
                    <Badge variant={node.k3sRole === "server" ? "default" : "secondary"} className="text-[10px] px-1.5 py-0 h-4.5">
                      {node.k3sRole}
                    </Badge>
                    <span className="text-xs text-muted-foreground tabular-nums">
                      {node.cpuCores ? `${node.cpuCores}c/${node.memoryGB}G` : "—"}
                    </span>
                  </div>
                </div>
              ))}
            </div>
          )}
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
        <NodeRegistrationTokenPanel />
        <HeadscalePreAuthKeyPanel />
        <K3sJoinTokenPanel />
      </div>
    </div>
  )
}

// ─── Registration token panel ─────────────────────────────────────────────────

function NodeRegistrationTokenPanel() {
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)
  const queryClient = useQueryClient()
  const [visible, setVisible] = useState(false)
  const [copied, setCopied] = useState(false)

  const { data, isLoading } = useQuery({
    queryKey: ["node-registration-token", orgId],
    queryFn: () => nodesApi.getRegistrationToken(orgId!, token),
    enabled: !!orgId,
  })

  const regToken = data?.token ?? ""

  const { mutate: generate, isPending: generating } = useMutation({
    mutationFn: () => nodesApi.generateRegistrationToken(orgId!, token),
    onSuccess: (res) => {
      queryClient.setQueryData(["node-registration-token", orgId], res)
      setVisible(true)
    },
  })

  const copy = async (text: string) => {
    await navigator.clipboard.writeText(text)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  const meshApiUrl = "http://100.64.0.1:4000"
  const curlCommand = regToken
    ? `curl -fsSL https://raw.githubusercontent.com/meshploy/meshploy/main/deploy/install.sh | \\\n  MESHPLOY_API_URL="${meshApiUrl}" MESHPLOY_TOKEN="${regToken}" bash`
    : ""

  return (
    <div className="rounded-lg border border-border/60 overflow-hidden">
      <div className="px-4 py-3 border-b border-border/40 bg-muted/20 flex items-center justify-between">
        <div>
          <p className="text-xs font-medium text-muted-foreground uppercase tracking-wider">Add a worker node</p>
        </div>
        <Button
          size="sm"
          variant="outline"
          className="h-7 text-xs gap-1.5"
          onClick={() => generate()}
          disabled={generating}
        >
          {generating ? (
            <Loader2 className="h-3 w-3 animate-spin" />
          ) : (
            <RefreshCw className="h-3 w-3" />
          )}
          {regToken ? "Rotate token" : "Generate token"}
        </Button>
      </div>

      <div className="p-4 space-y-4">
        {isLoading ? (
          <div className="flex items-center gap-2 text-muted-foreground text-sm">
            <Loader2 className="h-3.5 w-3.5 animate-spin" />
            <span>Loading…</span>
          </div>
        ) : !regToken ? (
          <p className="text-sm text-muted-foreground">
            Generate a token to get the worker install command.
          </p>
        ) : (
          <>
            {/* Token display */}
            <div className="space-y-1.5">
              <p className="text-xs text-muted-foreground font-medium">Registration token</p>
              <div className="flex items-center gap-2">
                <code className="flex-1 text-xs font-mono bg-muted/50 border border-border/40 rounded px-3 py-2 text-foreground overflow-hidden text-ellipsis whitespace-nowrap">
                  {visible ? regToken : "mreg-" + "•".repeat(64)}
                </code>
                <Button
                  size="icon"
                  variant="ghost"
                  className="h-8 w-8 shrink-0"
                  onClick={() => setVisible((v) => !v)}
                >
                  {visible ? <EyeOff className="h-3.5 w-3.5" /> : <Eye className="h-3.5 w-3.5" />}
                </Button>
                <Button
                  size="icon"
                  variant="ghost"
                  className="h-8 w-8 shrink-0"
                  onClick={() => copy(regToken)}
                >
                  {copied ? <Check className="h-3.5 w-3.5 text-emerald-400" /> : <Copy className="h-3.5 w-3.5" />}
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
                  onClick={() => copy(curlCommand.replace(/\\\n  /g, " "))}
                >
                  {copied ? <Check className="h-3 w-3 text-emerald-400" /> : <Copy className="h-3 w-3" />}
                </Button>
              </div>
              <p className="text-[11px] text-muted-foreground/60">
                The worker connects to the API over the WireGuard mesh ({meshApiUrl}) — no public internet needed after joining Headscale.
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
  const queryClient = useQueryClient()
  const [visible, setVisible] = useState(false)
  const [copied, setCopied] = useState(false)

  const { data, isLoading } = useQuery({
    queryKey: ["headscale-preauth-key"],
    queryFn: () => clusterApi.getHeadscalePreAuthKey(token),
  })

  const { mutate: generate, isPending: generating } = useMutation({
    mutationFn: () => clusterApi.createHeadscalePreAuthKey(token),
    onSuccess: (res) => {
      setVisible(true)
      queryClient.setQueryData(["headscale-preauth-key"], {
        has_active_key: true,
        key: res.key,
        headscale_url: res.headscale_url,
      })
    },
  })

  const copy = async (text: string) => {
    await navigator.clipboard.writeText(text)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
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
          disabled={generating || isLoading || unavailable}
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
                <Button size="icon" variant="ghost" className="h-8 w-8 shrink-0" onClick={() => copy(headscaleUrl)}>
                  {copied ? <Check className="h-3.5 w-3.5 text-emerald-400" /> : <Copy className="h-3.5 w-3.5" />}
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
                    <Button size="icon" variant="ghost" className="h-8 w-8 shrink-0" onClick={() => copy(activeKey)}>
                      {copied ? <Check className="h-3.5 w-3.5 text-emerald-400" /> : <Copy className="h-3.5 w-3.5" />}
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
                      onClick={() => copy(tailscaleCmd.replace(/\\\n  /g, " "))}
                    >
                      {copied ? <Check className="h-3 w-3 text-emerald-400" /> : <Copy className="h-3 w-3" />}
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
  const [visible, setVisible] = useState(false)
  const [copied, setCopied] = useState(false)

  const { data, isLoading } = useQuery({
    queryKey: ["cluster-join-token"],
    queryFn: () => clusterApi.getJoinToken(token),
  })

  const k3sToken = data?.token ?? ""
  const serverUrl = data?.server_url ?? "https://100.64.0.1:6443"

  const copy = async (text: string) => {
    await navigator.clipboard.writeText(text)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
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
                <Button size="icon" variant="ghost" className="h-8 w-8 shrink-0" onClick={() => copy(k3sToken)}>
                  {copied ? <Check className="h-3.5 w-3.5 text-emerald-400" /> : <Copy className="h-3.5 w-3.5" />}
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
                  onClick={() => copy(installCmd.replace(/\\\n  /g, " "))}
                >
                  {copied ? <Check className="h-3 w-3 text-emerald-400" /> : <Copy className="h-3 w-3" />}
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

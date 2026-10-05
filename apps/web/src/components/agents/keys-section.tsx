import { Link } from "@tanstack/react-router"
import { useEffect, useState } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Bot, ChevronRight, Key, Loader2, Plus, Shield, User } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from "@/components/ui/select"
import {
  agents as agentsApi,
  isTokenActive,
  type AgentDTO,
  type AgentRole,
} from "@/lib/api"
import { TokenRevealDialog } from "@/components/agents/token-reveal-dialog"
import { useMcpUrl } from "@/components/agents/use-mcp-url"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { formatRelativeTime } from "@/lib/utils"
import { Section } from "@/components/services/form-primitives"

/**
 * Keys, for CI, scripts and MCP clients that take a pasted token: each acts as
 * an agent, an identity with its own grants. On the Connectors page, for
 * owners and admins.
 */
export function KeysSection() {
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)!
  const qc = useQueryClient()
  const mcpUrl = useMcpUrl(orgId, token)

  const [search, setSearch] = useState("")
  const [showCreate, setShowCreate] = useState(false)
  const [revealToken, setRevealToken] = useState<string | null>(null)
  const [revealName, setRevealName] = useState<string>("")

  const { data: agents = [], isLoading } = useQuery({
    queryKey: ["agents", orgId],
    queryFn: () => agentsApi.list(orgId, token),
    enabled: !!orgId,
  })

  function onCreated(agentName: string, plaintext: string) {
    qc.invalidateQueries({ queryKey: ["agents", orgId] })
    setShowCreate(false)
    setRevealName(agentName)
    setRevealToken(plaintext)
  }

  const matches = agents.filter((a) => a.name.toLowerCase().includes(search.toLowerCase()))
  return (
    <Section
      title="Keys"
      subtitle="For CI, scripts and MCP clients that take a pasted token. Each key acts as an agent: an identity with its own grants."
      action={
        <Button size="sm" variant="outline" className="h-7 shrink-0 gap-1.5 text-xs" onClick={() => setShowCreate(true)}>
          <Plus className="h-3.5 w-3.5" />New key
        </Button>
      }
    >
      <div className="space-y-4">
        {agents.length > 0 && (
          <Input aria-label="Search agents and keys" placeholder="Search agents and keys…" value={search} onChange={(e) => setSearch(e.target.value)} className="h-10 max-w-md" />
        )}
        {isLoading ? (
          <div className="flex items-center gap-2 text-sm text-muted-foreground">
            <Loader2 className="h-3.5 w-3.5 animate-spin" /><span>Loading…</span>
          </div>
        ) : agents.length === 0 ? (
          <div className="flex flex-col items-center gap-3 rounded-lg border border-dashed border-border/50 py-10 text-center">
            <div className="flex h-10 w-10 items-center justify-center rounded-full bg-muted/40"><Bot className="h-5 w-5 text-muted-foreground" /></div>
            <div className="space-y-1">
              <p className="text-sm font-medium">No keys yet</p>
              <p className="max-w-xs text-xs text-muted-foreground">A key lets CI, a script or an MCP client that takes a token act in this organisation, as an agent with the access you give it.</p>
            </div>
          </div>
        ) : matches.length === 0 ? (
          <p className="text-sm text-muted-foreground">No agent or key matches.</p>
        ) : (
          <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
            {matches.map((agent) => <AgentRow key={agent.id} agent={agent} />)}
          </div>
        )}
      </div>

      <CreateAgentDialog open={showCreate} onOpenChange={setShowCreate} orgId={orgId} token={token} onCreated={onCreated} />
      <TokenRevealDialog open={!!revealToken} onOpenChange={(o) => { if (!o) setRevealToken(null) }} token={revealToken} agentName={revealName} mcpUrl={mcpUrl} />
    </Section>
  )
}

// ---------------------------------------------------------------------------

function AgentRow({ agent }: { agent: AgentDTO }) {
  const activeTokens = agent.tokens.filter(isTokenActive).length
  const lastUsedMs = agent.tokens
    .map((t) => (t.last_used_at ? new Date(t.last_used_at).getTime() : 0))
    .reduce((a, b) => Math.max(a, b), 0)

  return <Link to="/agents/$agentId" params={{agentId:agent.id}} className="listing-surface interactive-surface rounded-xl border border-border p-5 flex flex-col gap-5 min-w-0"><div className="flex items-center justify-between"><span className="accent-icon-tile"><Bot className="size-5" /></span><RoleBadge role={agent.role}/></div><div><h2 className="font-semibold break-words">{agent.name}</h2><p className="text-xs text-muted-foreground mt-2">{agent.role === "admin" ? "Organization-wide administration" : "Access through assigned resource permissions"}</p></div><div className="border-t border-border pt-4 flex flex-wrap gap-3 justify-between text-xs text-muted-foreground"><span className="inline-flex gap-2 items-center"><Key className="size-3"/>{activeTokens} active {activeTokens === 1 ? "key" : "keys"}</span><span>{lastUsedMs ? `Used ${formatRelativeTime(new Date(lastUsedMs))}` : "Never used"}</span></div><span className="text-xs text-primary inline-flex justify-between items-center">Manage access & connection<ChevronRight className="size-4"/></span></Link>
}

function RoleBadge({ role }: { role: AgentRole }) {
  if (role === "admin") return (
    <Badge className="gap-1 text-[11px] px-1.5 py-0 h-5 bg-primary/10 text-primary border-primary/20 hover:bg-primary/10 shrink-0">
      <Shield className="h-2.5 w-2.5" />admin
    </Badge>
  )
  return (
    <Badge variant="secondary" className="gap-1 text-[11px] px-1.5 py-0 h-5 shrink-0">
      <User className="h-2.5 w-2.5" />member
    </Badge>
  )
}

// ---------------------------------------------------------------------------

function CreateAgentDialog({ open, onOpenChange, orgId, token, onCreated }: {
  open: boolean
  onOpenChange: (open: boolean) => void
  orgId: string
  token: string
  onCreated: (agentName: string, plaintext: string) => void
}) {
  const [name, setName] = useState("")
  const [role, setRole] = useState<AgentRole>("member")
  const [tokenName, setTokenName] = useState("")
  const [expiresAt, setExpiresAt] = useState("")

  useEffect(() => {
    if (open) {
      setName(""); setRole("member"); setTokenName(""); setExpiresAt("")
    }
  }, [open])

  const { mutate, isPending, error } = useMutation({
    mutationFn: () => agentsApi.create(orgId, {
      name: name.trim(),
      role,
      token_name: tokenName.trim() || undefined,
      expires_at: expiresAt ? new Date(expiresAt).toISOString() : undefined,
    }, token),
    onSuccess: (res) => onCreated(res.agent.name, res.token),
  })

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>New key</DialogTitle>
          <DialogDescription>
            A key acts as an agent: a name for what uses it, with its own access. The key is shown once.
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-5">
          <div className="flex flex-col gap-3">
            <label className="text-xs font-medium text-muted-foreground">Agent <span className="text-muted-foreground/50">(what uses the key)</span></label>
            <Input
              placeholder="ci-deploy"
              value={name}
              onChange={(e) => setName(e.target.value)}
              className="h-9 text-sm"
              autoFocus
            />
          </div>

          <div className="flex flex-col gap-3">
            <label className="text-xs font-medium text-muted-foreground">Role</label>
            <Select value={role} onValueChange={(v) => v && setRole(v as AgentRole)}>
              <SelectTrigger className="w-full h-9 text-sm bg-muted/20 border-border/60">
                <SelectValue>{role}</SelectValue>
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="member">member</SelectItem>
                <SelectItem value="admin">admin</SelectItem>
              </SelectContent>
            </Select>
          </div>

          <div className="grid grid-cols-2 gap-3">
            <div className="flex flex-col gap-3">
              <label className="text-xs font-medium text-muted-foreground">Key name <span className="text-muted-foreground/50">(optional)</span></label>
              <Input
                placeholder="default"
                value={tokenName}
                onChange={(e) => setTokenName(e.target.value)}
                className="h-9 text-sm"
              />
            </div>
            <div className="flex flex-col gap-3">
              <label className="text-xs font-medium text-muted-foreground">Expires <span className="text-muted-foreground/50">(optional)</span></label>
              <Input
                type="date"
                value={expiresAt}
                onChange={(e) => setExpiresAt(e.target.value)}
                className="h-9 text-sm"
              />
            </div>
          </div>

          {error && (
            <p className="text-xs text-destructive">{(error as Error).message}</p>
          )}
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>Cancel</Button>
          <Button onClick={() => mutate()} disabled={isPending || !name.trim()} className="gap-1.5">
            {isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Plus className="h-3.5 w-3.5" />}
            Create key
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

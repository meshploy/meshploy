import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import { useState } from "react"
import { Info, Loader2, ShieldAlert, ShieldCheck } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Switch } from "@/components/ui/switch"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { formatRelativeTime } from "@/lib/utils"
import { ResourcePanel } from "@/components/layout/resource-workbench"
import { OptionSelect } from "@/components/layout/option-select"
import { nodes as nodesApi, orgs as orgsApi, type MeshMachine, type MeshReach } from "@/lib/api"
import type { Node } from "@/types"

// What the mesh would allow, generated from who owns each machine and what
// they may use. Shown before it is enforced, so an install sees what would be
// blocked first.

const NOT_ENFORCED = "Not enforced yet: today every machine on the mesh reaches every other on every port."

function useMeshAccess(orgId: string, token: string) {
  return useQuery({ queryKey: ["mesh-access", orgId], queryFn: () => nodesApi.meshAccess(orgId, token) })
}

function ports(r: MeshReach) {
  return r.ports?.length ? r.ports.join(", ") : "every port"
}

/** One machine's lines: everything, or each place and port with the reason. */
function Reaches({ machine }: { machine: MeshMachine }) {
  if (machine.everything) {
    return <p className="text-sm">Every machine, every port <span className="text-muted-foreground">· {machine.reaches.find((r) => r.to === "every machine")?.why}</span></p>
  }
  if (machine.reaches.length === 0) return <p className="text-sm text-muted-foreground">Nothing</p>
  return (
    <ul className="space-y-1.5">
      {machine.reaches.map((r, i) => (
        <li key={i} className="text-sm">
          <span className="font-medium">{r.to}</span>
          <span className="font-mono text-xs text-muted-foreground"> · {ports(r)}</span>
          <p className="text-xs text-muted-foreground">{r.why}</p>
        </li>
      ))}
    </ul>
  )
}

/**
 * A connected machine's owner, and what it would reach on the mesh as theirs.
 * An admin can give it to someone else.
 */
export function MeshAccessPanel({ node, orgId, token, isAdmin }: { node: Node; orgId: string; token: string; isAdmin: boolean }) {
  const qc = useQueryClient()
  const access = useMeshAccess(orgId, token)
  const members = useQuery({ queryKey: ["members", orgId], queryFn: () => orgsApi.listMembers(orgId, token), enabled: isAdmin })
  const setOwner = useMutation({
    mutationFn: (ownerId: string) => nodesApi.setOwner(orgId, node.id, ownerId, token),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["mesh-access", orgId] })
      qc.invalidateQueries({ queryKey: ["node", orgId, node.id] })
      qc.invalidateQueries({ queryKey: ["nodes", orgId] })
    },
  })
  const machine = access.data?.machines.find((m) => m.id === node.id)
  const owner = node.ownerId ?? ""
  const options = [
    { value: "", label: "The organisation" },
    ...(members.data ?? []).map((m) => ({ value: m.user_id, label: m.user_name })),
  ]

  return (
    <ResourcePanel title="Mesh access" description="Whose machine this is, and what it would reach on the mesh as theirs.">
      <div className="space-y-4">
        <div className="space-y-1.5">
          <p className="text-xs font-medium text-muted-foreground">Owner</p>
          {isAdmin ? (
            <div className="flex items-center gap-2">
              <OptionSelect label="Owner" value={owner} onChange={(v) => setOwner.mutate(v)} options={options} className="w-full" />
              {setOwner.isPending && <Loader2 className="size-3.5 animate-spin text-muted-foreground" />}
            </div>
          ) : (
            <p className="text-sm">{machine?.owner_name || "The organisation"}</p>
          )}
          {setOwner.error && <p role="alert" className="text-xs text-destructive">{setOwner.error.message}</p>}
        </div>
        <div className="space-y-1.5">
          <p className="text-xs font-medium text-muted-foreground">Would reach</p>
          {access.isLoading ? <Loader2 className="size-3.5 animate-spin text-muted-foreground" /> : machine ? <Reaches machine={machine} /> : <p className="text-sm text-muted-foreground">Not on the mesh yet</p>}
        </div>
        <p className="flex items-start gap-1.5 text-xs text-muted-foreground">
          <Info className="mt-0.5 size-3 shrink-0" />{access.data?.enforced ? "Enforced: this is what the mesh lets it reach." : NOT_ENFORCED}
        </p>
      </div>
    </ResourcePanel>
  )
}

const KIND: Record<MeshMachine["kind"], string> = { gateway: "Gateway", cluster: "Cluster", connected: "Connected" }

/**
 * Whether the mesh obeys the policy: the switch for the server's owner, and
 * how giving it to Headscale last went, for everyone who can see the report.
 */
function Enforcement({ orgId, token, enforced, appliedAt, lastError, canEnforce, unknown }: {
  orgId: string; token: string; enforced: boolean; appliedAt?: string; lastError?: string; canEnforce: boolean; unknown: number
}) {
  const qc = useQueryClient()
  const [asking, setAsking] = useState<boolean | null>(null)
  const set = useMutation({
    mutationFn: (on: boolean) => nodesApi.setMeshEnforced(orgId, on, token),
    onSuccess: () => {
      setAsking(null)
      // Headscale is given the policy within a second or two; read it again then.
      setTimeout(() => qc.invalidateQueries({ queryKey: ["mesh-access", orgId] }), 2500)
      qc.invalidateQueries({ queryKey: ["mesh-access", orgId] })
    },
  })
  return (
    <div className="space-y-2">
      <div className={`flex items-start gap-3 rounded-lg border px-3 py-2.5 ${enforced ? "border-primary/30 bg-primary/5" : "border-border/60 bg-muted/10"}`}>
        {enforced ? <ShieldCheck className="mt-0.5 size-4 shrink-0 text-primary" /> : <Info className="mt-0.5 size-4 shrink-0 text-muted-foreground" />}
        <div className="min-w-0 flex-1">
          <p className="text-sm font-medium">{enforced ? "Enforced" : "Not enforced"}</p>
          <p className="text-xs text-muted-foreground">
            {enforced
              ? `The mesh lets each machine reach only what is listed here.${appliedAt ? ` Headscale took the latest policy ${formatRelativeTime(new Date(appliedAt))}.` : ""}`
              : "Every machine on the mesh reaches every other on every port. The list below is what each would reach once enforced."}
          </p>
        </div>
        {canEnforce && (
          <Switch aria-label="Enforce the mesh access policy" checked={enforced} disabled={set.isPending} onCheckedChange={(v) => setAsking(Boolean(v))} />
        )}
      </div>
      {lastError && (
        <p role="alert" className="flex items-start gap-1.5 rounded-md border border-destructive/20 bg-destructive/10 px-3 py-2 text-xs text-destructive">
          <ShieldAlert className="mt-0.5 size-3.5 shrink-0" />
          Headscale did not take the policy: {lastError}. On a server upgraded before the mesh had a policy, run its upgrade again to switch Headscale to keeping the policy itself.
        </p>
      )}
      {set.error && <p role="alert" className="text-xs text-destructive">{set.error.message}</p>}
      <Dialog open={asking !== null} onOpenChange={(o) => { if (!o) setAsking(null) }}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{asking ? "Enforce the mesh access policy?" : "Stop enforcing it?"}</DialogTitle>
            <DialogDescription>
              {asking
                ? `Each machine will reach only what this list says, within a few seconds, for every organisation on this server. Owners' and admins' machines keep reaching everything.${unknown > 0 ? ` ${unknown} machine${unknown === 1 ? "" : "s"} Meshploy does not know will reach nothing.` : ""} You can switch it off here at any time.`
                : "Every machine on the mesh will reach every other on every port again, within a few seconds."}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setAsking(null)}>Cancel</Button>
            <Button variant={asking ? "default" : "destructive"} disabled={set.isPending} onClick={() => set.mutate(Boolean(asking))}>
              {set.isPending && <Loader2 className="size-3.5 animate-spin" />}
              {asking ? "Enforce" : "Stop enforcing"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}

/** Every machine and what it would reach, for an admin to read before the mesh obeys it. */
export function MeshAccessReport({ orgId, token }: { orgId: string; token: string }) {
  const access = useMeshAccess(orgId, token)
  if (access.isLoading || !access.data) return null
  const people = access.data.machines.filter((m) => m.kind === "connected")
  const unknown = access.data.unknown ?? []
  return (
    <ResourcePanel
      title="Mesh access"
      description="What each person's machine reaches on the mesh: what its owner may use in the console, on that thing's own port. Owners and admins reach everything; the cluster's own traffic is always allowed."
    >
      <div className="space-y-3">
        <Enforcement orgId={orgId} token={token} enforced={access.data.enforced} appliedAt={access.data.applied_at}
          lastError={access.data.last_error} canEnforce={access.data.can_enforce} unknown={unknown.length} />
        {unknown.length > 0 && (
          <div role="status" className="rounded-lg border border-amber-500/20 bg-amber-500/5 px-3 py-2 text-xs text-amber-300/90">
            <p className="font-medium">Machines Meshploy does not know</p>
            <p className="mt-0.5">Joined with a Headscale key by hand, so they have no owner and the policy gives them nothing: {unknown.map((u) => `${u.name} (${u.ips[0] ?? "no address"})`).join(", ")}. Join them through Add a node to give them an owner, or remove them.</p>
          </div>
        )}
        {people.length === 0 ? (
          <p className="text-sm text-muted-foreground">No connected machines. Laptops and servers joined as mesh-only appear here, with what their owner may reach.</p>
        ) : (
          <div className="console-record-list divide-y divide-border/40 overflow-hidden rounded-xl border border-border">
            {people.map((m) => (
              <div key={m.id} className="grid gap-3 px-4 py-3 sm:grid-cols-[minmax(0,14rem)_1fr]">
                <div className="min-w-0">
                  <Link to="/nodes/$id" params={{ id: m.id }} className="truncate text-sm font-medium hover:underline">{m.name}</Link>
                  <p className="font-mono text-xs text-muted-foreground">{m.ip}</p>
                  <p className="text-xs text-muted-foreground">{m.owner_name ? `${m.owner_name}'s` : "The organisation's: set an owner"}</p>
                </div>
                <Reaches machine={m} />
              </div>
            ))}
          </div>
        )}
        <p className="text-xs text-muted-foreground">
          {access.data.machines.filter((m) => m.kind !== "connected").map((m) => `${m.name} (${KIND[m.kind].toLowerCase()})`).join(", ")}: the cluster's machines reach each other, and the gateway reaches every machine.
        </p>
      </div>
    </ResourcePanel>
  )
}

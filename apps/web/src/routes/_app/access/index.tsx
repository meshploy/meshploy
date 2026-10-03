import { createFileRoute, Link, useNavigate } from "@tanstack/react-router"
import { useEffect, useMemo, useState } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Copy, Loader2, Pencil, Plus, Search, Trash2, X } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Switch } from "@/components/ui/switch"
import { SegmentedControl } from "@/components/ui/segmented-control"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { ResourcePanel } from "@/components/layout/resource-workbench"
import { OptionSelect } from "@/components/layout/option-select"
import { Field, inputCls } from "@/components/services/form-primitives"
import { MeshAccessReport } from "@/components/nodes/mesh-access"
import { EeOutsiderGrant } from "@/ee"
import {
  access as accessApi, nodes as nodesApi, orgs as orgsApi, permissions as permissionsApi,
  projects as projectsApi, services as servicesApi, stacks as stacksApi,
  RESOURCE_ACTIONS, type AccessRule, type NetworkRuleBody, type ResourceAction,
} from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"

// Who reaches what, from one place: every grant and every network rule as
// Source -> Destination -> Ports, what each machine reaches, and the policy the
// mesh is given. A grant here is the same grant a resource's Access tab shows.

export const Route = createFileRoute("/_app/access/")({
  // person narrows the rules to one person's, so a member's page can link to
  // theirs; q fills the search box.
  validateSearch: (search: Record<string, unknown>): { q?: string; person?: string } => ({
    q: typeof search.q === "string" && search.q ? search.q : undefined,
    person: typeof search.person === "string" && search.person ? search.person : undefined,
  }),
  component: AccessPage,
})

type Tab = "rules" | "machines" | "policy"

function AccessPage() {
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)
  const [tab, setTab] = useState<Tab>("rules")
  const { q, person } = Route.useSearch()
  if (!orgId) return null
  return (
    <div className="console-page space-y-6 p-6">
      <div>
        <h1 className="text-xl font-semibold tracking-tight">Access</h1>
        <p className="mt-0.5 text-sm text-muted-foreground">Who may reach what on your mesh, and on which ports</p>
      </div>
      <SegmentedControl<Tab>
        value={tab}
        onValueChange={setTab}
        options={[
          { value: "rules", label: "Rules" },
          { value: "machines", label: "By machine" },
          { value: "policy", label: "Policy" },
        ]}
      />
      {tab === "rules" && <Rules orgId={orgId} token={token} initialSearch={q ?? ""} person={person} />}
      {tab === "machines" && <MeshAccessReport orgId={orgId} token={token} />}
      {tab === "policy" && <Policy orgId={orgId} token={token} />}
    </div>
  )
}

// ── Rules ─────────────────────────────────────────────────────────────────────

// One column template for the header and every row, the actions column fixed,
// so a row with an Open link lines up with one without.
const COLS = "md:grid-cols-[minmax(0,1fr)_minmax(0,1.1fr)_minmax(0,1.4fr)_minmax(0,1.1fr)_5.5rem]"

const KIND_LABEL: Record<string, string> = {
  person: "Person", machine: "Machine", all: "Every connected machine", service: "Service", stack: "Stack",
  project: "Project", job: "Job", route: "Route",
}

/** What a network rule opens: its ports. */
function portsText(r: AccessRule) {
  return r.ports.length ? r.ports.join(", ") : "All ports"
}

const MAX_OPENS = 3

/**
 * What a grant opens from its person's machines: each place it answers on the
 * mesh and each internal route to it. Someone outside the organisation owns no
 * machine on the mesh, so their grant opens nothing there.
 */
function Opens({ r, orgId }: { r: AccessRule; orgId: string }) {
  const muted = "text-xs text-muted-foreground"
  if (r.kind === "network") return <p className={`${muted} ${r.ports.length ? "font-mono" : ""}`}>{portsText(r)}</p>
  if (!r.from.member) {
    if (EeOutsiderGrant) return <EeOutsiderGrant orgId={orgId} email={r.from.email} resourceKind={r.to.kind} resourceId={r.to.id} projectId={r.to.project_id} />
    return <p className={muted}>Not a member: no machines on the mesh</p>
  }
  if (r.to.kind === "job") return <p className={muted}>-</p>
  const opens = r.opens ?? []
  if (opens.length === 0) {
    return <p className={muted}>{r.off ? "Switched off from their machines" : "Nothing on the mesh"}</p>
  }
  const shown = opens.slice(0, MAX_OPENS)
  return (
    <ul className={`min-w-0 space-y-0.5 ${muted}`}>
      {shown.map((o, i) => (
        <li key={i} className="truncate" title={o.hostname}>
          {o.service && <span>{o.service}: </span>}
          {o.kind === "port"
            ? <><span className="font-mono text-foreground/80">{o.port}</span> on {o.on === "gateway" ? "the gateway" : "the cluster"}</>
            : <span className="font-mono text-foreground/80">{o.hostname}</span>}
        </li>
      ))}
      {(opens.length > MAX_OPENS || (r.off ?? 0) > 0) && (
        <li>
          {opens.length > MAX_OPENS && `${opens.length - MAX_OPENS} more`}
          {opens.length > MAX_OPENS && (r.off ?? 0) > 0 && " · "}
          {(r.off ?? 0) > 0 && `${r.off} switched off`}
        </li>
      )}
    </ul>
  )
}

/** A rule's person or machine, linked to its own page. */
function EndLink({ end, fallback }: { end: AccessRule["from"]; fallback: string }) {
  const name = end.name || fallback
  if (end.kind === "person" && end.id && end.member) return <Link to="/users/$userId" params={{ userId: end.id }} className="hover:underline">{name}</Link>
  if (end.kind === "machine" && end.id) return <Link to="/nodes/$id" params={{ id: end.id }} className="hover:underline">{name}</Link>
  return <>{name}</>
}

/** Where a grant is edited in full: the resource's own Access tab. */
function grantLink(r: AccessRule) {
  const p = r.to.project_id
  if (!p || !r.to.id) return null
  switch (r.to.kind) {
    case "service": return <Link to="/projects/$id/services/$serviceId/permissions" params={{ id: p, serviceId: r.to.id }} className="text-xs text-primary hover:underline">Open</Link>
    case "stack": return <Link to="/projects/$id/stacks/$stackId/permissions" params={{ id: p, stackId: r.to.id }} className="text-xs text-primary hover:underline">Open</Link>
    case "job": return <Link to="/projects/$id/jobs/$jobId/permissions" params={{ id: p, jobId: r.to.id }} className="text-xs text-primary hover:underline">Open</Link>
    default: return null
  }
}

function Rules({ orgId, token, initialSearch, person }: { orgId: string; token: string; initialSearch: string; person?: string }) {
  const qc = useQueryClient()
  const navigate = useNavigate()
  const [search, setSearch] = useState(initialSearch)
  const [adding, setAdding] = useState(false)
  const [editing, setEditing] = useState<AccessRule | null>(null)
  const { data: rules = [], isLoading } = useQuery({ queryKey: ["access-rules", orgId], queryFn: () => accessApi.rules(orgId, token) })
  const refresh = () => {
    qc.invalidateQueries({ queryKey: ["access-rules", orgId] })
    qc.invalidateQueries({ queryKey: ["mesh-access", orgId] })
  }
  const remove = useMutation({
    mutationFn: async (r: AccessRule) => {
      if (r.kind === "network") return accessApi.removeRule(orgId, r.id, token)
      // A grant goes with every action it carries.
      for (const action of r.actions ?? []) {
        await permissionsApi.revoke(orgId, r.from.id!, { resource_type: r.to.kind as never, resource_id: r.to.id!, action: action as ResourceAction }, token)
      }
    },
    onSuccess: refresh,
  })
  const personName = person ? rules.find((r) => r.from.id === person)?.from.name : undefined
  const shown = rules.filter((r) => !person || r.from.id === person).filter((r) =>
    `${r.from.name} ${r.from.email ?? ""} ${r.to.name} ${r.ports.join(" ")} ${(r.opens ?? []).map((o) => o.hostname ?? "").join(" ")} ${r.note ?? ""} ${KIND_LABEL[r.to.kind]}`.toLowerCase().includes(search.toLowerCase()))

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-3">
        <div className="relative max-w-md flex-1">
          <Search className="absolute left-3 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input aria-label="Search rules" placeholder="Search by person, machine, service, port…" value={search} onChange={(e) => setSearch(e.target.value)} className="h-10 pl-8" />
        </div>
        <Button className="sm:ml-auto" onClick={() => setAdding(true)}><Plus className="size-4" />Add rule</Button>
      </div>
      {person && (
        <p className="flex items-center gap-2 text-xs">
          <span className="inline-flex items-center gap-1.5 rounded-full border border-primary/30 bg-primary/10 px-2.5 py-1 text-primary">
            Showing {personName ? `${personName}'s` : "one person's"} rules
            <button type="button" aria-label="Show every rule" className="hover:text-foreground" onClick={() => navigate({ to: "/access", search: {} })}><X className="size-3" /></button>
          </span>
        </p>
      )}
      <p className="text-xs text-muted-foreground">Owners and admins reach everything, and the cluster's own traffic is always allowed; these rules are everyone else.</p>
      {isLoading ? (
        <Loader2 className="size-4 animate-spin text-muted-foreground" />
      ) : shown.length === 0 ? (
        <div className="rounded-xl border border-dashed border-border/60 py-10 text-center text-sm text-muted-foreground">
          {rules.length === 0 ? "No rules yet. Add one to let a person or a machine reach something." : "No rule matches."}
        </div>
      ) : (
        <div className="console-record-list overflow-hidden rounded-xl border border-border">
          <div className={`hidden ${COLS} gap-4 border-b border-border/40 bg-muted/20 px-4 py-2.5 text-xs text-muted-foreground md:grid`}>
            <span>Source</span><span>can reach</span><span>opens on the mesh</span><span /><span />
          </div>
          <div className="divide-y divide-border/40">
            {shown.map((r) => (
              <div key={r.id} className={`grid gap-2 px-4 py-3 ${COLS} md:items-center md:gap-4`}>
                <div className="min-w-0">
                  <p className="truncate text-sm font-medium"><EndLink end={r.from} fallback="Someone removed" /></p>
                  <p className="truncate text-xs text-muted-foreground">{r.from.email || KIND_LABEL[r.from.kind]}</p>
                </div>
                <div className="min-w-0">
                  <p className="truncate text-sm"><EndLink end={r.to} fallback="Something removed" /></p>
                  <p className="text-xs text-muted-foreground">{r.to.database ? "Database" : KIND_LABEL[r.to.kind]}</p>
                </div>
                <Opens r={r} orgId={orgId} />
                <div className="min-w-0 text-xs text-muted-foreground">
                  {r.kind === "grant" ? (
                    <>
                      <span className="mr-2 inline-flex flex-wrap gap-1">{(r.actions ?? []).map((a) => <span key={a} className="rounded border border-primary/30 bg-primary/10 px-1.5 text-[11px] text-primary">{a}</span>)}</span>
                      {/* The mesh is for members' machines: someone who is not a
                          member owns none, so their grant is the console's alone. */}
                      {r.from.member && r.to.kind !== "job" && r.to.kind !== "route" && (
                        <span>{r.reach === undefined ? "Machines: default" : r.reach ? "Machines: can connect" : "Machines: console only"}</span>
                      )}
                    </>
                  ) : (
                    <span>{r.note || "Network rule"}</span>
                  )}
                </div>
                <div className="flex items-center justify-end gap-1">
                  {r.kind === "grant" && <span className="mr-2">{grantLink(r)}</span>}
                  <Button variant="ghost" size="sm" className="h-7 px-2 text-xs text-muted-foreground hover:text-foreground" aria-label={`Edit ${r.from.name} to ${r.to.name}`}
                    onClick={() => setEditing(r)}>
                    <Pencil className="size-3.5" />
                  </Button>
                  <Button variant="ghost" size="sm" className="h-7 px-2 text-xs text-muted-foreground hover:text-destructive" aria-label={`Remove ${r.from.name} to ${r.to.name}`}
                    disabled={remove.isPending && remove.variables?.id === r.id} onClick={() => remove.mutate(r)}>
                    {remove.isPending && remove.variables?.id === r.id ? <Loader2 className="size-3.5 animate-spin" /> : <Trash2 className="size-3.5" />}
                  </Button>
                </div>
              </div>
            ))}
          </div>
        </div>
      )}
      {remove.error && <p role="alert" className="text-xs text-destructive">{remove.error.message}</p>}
      <AddRule orgId={orgId} token={token} open={adding || editing?.kind === "network"} editing={editing?.kind === "network" ? editing : null}
        onOpenChange={(o) => { if (!o) { setAdding(false); setEditing(null) } }} onAdded={refresh} />
      <GrantEditor orgId={orgId} token={token} rule={editing?.kind === "grant" ? editing : null} onClose={() => setEditing(null)} onChanged={refresh} />
    </div>
  )
}

// ── Adding a rule ─────────────────────────────────────────────────────────────

type DestKind = "resource" | "machine"

/**
 * One form for both kinds: a Meshploy resource makes a grant (a person may
 * use it, and whether their machines connect to it); a machine makes a network
 * rule (ports on it for a person, a machine or every machine).
 */
function AddRule({ orgId, token, open, editing, onOpenChange, onAdded }: {
  orgId: string; token: string; open: boolean; editing: AccessRule | null; onOpenChange: (o: boolean) => void; onAdded: () => void
}) {
  const [dest, setDest] = useState<DestKind>("resource")
  const [from, setFrom] = useState("")
  const [project, setProject] = useState("")
  const [item, setItem] = useState("")
  const [machine, setMachine] = useState("")
  const [ports, setPorts] = useState("")
  const [note, setNote] = useState("")
  const [reach, setReach] = useState<boolean | null>(null)
  useEffect(() => {
    if (!open) return
    setProject(""); setItem(""); setReach(null)
    if (editing) {
      // A network rule, as it stands: its machine, source, ports and note.
      setDest("machine")
      setFrom(editing.from.kind === "all" ? "all" : `${editing.from.kind}:${editing.from.id}`)
      setMachine(editing.to.id ?? ""); setPorts(editing.ports.join(", ")); setNote(editing.note ?? "")
    } else {
      setDest("resource"); setFrom(""); setMachine(""); setPorts(""); setNote("")
    }
  }, [open, editing])

  const members = useQuery({ queryKey: ["members", orgId], queryFn: () => orgsApi.listMembers(orgId, token), enabled: open })
  const machines = useQuery({ queryKey: ["nodes", orgId], queryFn: () => nodesApi.list(orgId, token), enabled: open })
  const projects = useQuery({ queryKey: ["projects", orgId, "access"], queryFn: () => projectsApi.list(orgId, token), enabled: open && dest === "resource" })
  const services = useQuery({ queryKey: ["services", orgId, project], queryFn: () => servicesApi.list(orgId, project, token), enabled: open && !!project })
  const stacks = useQuery({ queryKey: ["stacks", orgId, project], queryFn: () => stacksApi.list(orgId, project, token), enabled: open && !!project })

  const people = (members.data ?? []).filter((m) => m.role === "member")
  const sourceOptions = [
    { value: "", label: "Choose who" },
    ...people.map((m) => ({ value: `person:${m.user_id}`, label: `${m.user_name} (person)` })),
    // Only connected machines: the gateway and the cluster's machines reach
    // every machine already, so a rule from one would add nothing.
    ...(dest === "machine" ? [
      { value: "all", label: "Every connected machine" },
      ...(machines.data ?? []).filter((n) => n.mesh_role === "mesh").map((n) => ({ value: `machine:${n.id}`, label: `${n.name} (connected machine)` })),
    ] : []),
  ]
  const [fromKind, fromId] = from.includes(":") ? from.split(":") : [from, ""]

  // The resource chosen, and the service behind it when it is one.
  const svc = item.startsWith("service:") ? (services.data ?? []).find((s) => s.id === item.slice(8)) : undefined
  const isDatabase = svc?.type === "database"
  const reachNow = reach ?? !isDatabase
  const meshPorts = (svc?.ports ?? []).filter((p) => p.is_public).map((p) => p.port)

  const body: NetworkRuleBody | null = dest === "machine" && machine && fromKind
    ? { from_kind: fromKind as NetworkRuleBody["from_kind"], from_id: fromId || undefined, to_node_id: machine, ports, note }
    : null
  const preview = useQuery({
    queryKey: ["access-preview", orgId, body],
    queryFn: () => accessApi.previewRule(orgId, body!, token),
    enabled: open && !!body,
    retry: false,
  })

  const save = useMutation({
    mutationFn: async () => {
      if (editing) return accessApi.updateRule(orgId, editing.id, body!, token)
      if (dest === "machine") return accessApi.addRule(orgId, body!, token)
      const [kind, id] = item ? item.split(":") : ["project", project]
      await permissionsApi.grant(orgId, fromId, { resource_type: kind as never, resource_id: id, action: "view" }, token)
      // Only a choice someone made is stored; the default needs no row.
      if (reach !== null) {
        await nodesApi.setMeshReach(orgId, { user_id: fromId, resource_type: kind as "service" | "stack" | "project", resource_id: id, reach }, token)
      }
    },
    onSuccess: () => { onAdded(); onOpenChange(false) },
  })
  const ready = dest === "machine" ? !!body : fromKind === "person" && !!project

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle>{editing ? "Edit rule" : "Add rule"}</DialogTitle>
          <DialogDescription>Let a person or a connected machine reach something on the mesh. The gateway and the cluster's machines reach every machine already.</DialogDescription>
        </DialogHeader>
        <div className="grid gap-6 md:grid-cols-[1fr_1fr]">
          <div className="space-y-4">
            {!editing && <SegmentedControl<DestKind>
              value={dest}
              onValueChange={(v) => { setDest(v); setFrom("") }}
              options={[{ value: "resource", label: "A service or project" }, { value: "machine", label: "A machine" }]}
            />}
            <Field label="Source">
              <OptionSelect label="Source" value={from} onChange={setFrom} options={sourceOptions} className="w-full" />
            </Field>
            {dest === "resource" ? (
              <>
                <Field label="Project">
                  <OptionSelect label="Project" value={project} onChange={(v) => { setProject(v); setItem("") }} className="w-full"
                    options={[{ value: "", label: "Choose a project" }, ...(projects.data ?? []).filter((p) => !p.parent_project_id).map((p) => ({ value: p.id, label: p.name }))]} />
                </Field>
                {project && (
                  <Field label="In it">
                    <OptionSelect label="In it" value={item} onChange={(v) => { setItem(v); setReach(null) }} className="w-full"
                      options={[
                        { value: "", label: "The whole project" },
                        ...(services.data ?? []).map((s) => ({ value: `service:${s.id}`, label: `${s.name} (${s.type === "database" ? "database" : "service"})` })),
                        ...(stacks.data ?? []).map((s) => ({ value: `stack:${s.id}`, label: `${s.name} (stack)` })),
                      ]} />
                  </Field>
                )}
                {project && (
                  <div className="flex items-start gap-3 rounded-lg border border-border/40 bg-muted/10 px-3 py-2.5">
                    <div className="min-w-0 flex-1">
                      <p className="text-xs font-medium">From their machines</p>
                      <p className="text-xs text-muted-foreground">
                        {svc
                          ? meshPorts.length ? `Connect to ${svc.name} on ${meshPorts.join(", ")}, and open its internal routes.${isDatabase && reach === null ? " Off by default for a database." : ""}` : `Open ${svc.name}'s internal routes; it publishes no port on the mesh.`
                          : "Connect to the published ports and internal routes of what this covers. Databases stay off: each is switched on by itself."}
                      </p>
                    </div>
                    <Switch aria-label="From their machines" checked={reachNow} onCheckedChange={(v) => setReach(Boolean(v))} />
                  </div>
                )}
                <p className="text-xs text-muted-foreground">They get <span className="font-medium text-foreground">view</span> in the console; change what else they may do on its Access tab.</p>
              </>
            ) : (
              <>
                <Field label="Destination">
                  <OptionSelect label="Destination" value={machine} onChange={setMachine} className="w-full"
                    options={[{ value: "", label: "Choose a machine" }, ...(machines.data ?? []).map((n) => ({ value: n.id, label: `${n.name} (${n.tailscale_ip})` }))]} />
                </Field>
                <Field label="Ports">
                  <input className={inputCls} placeholder="All ports, or for example 22, 445" value={ports} onChange={(e) => setPorts(e.target.value)} />
                </Field>
                <Field label="Note">
                  <textarea className={`${inputCls} h-20 py-2`} placeholder="Why this rule exists" value={note} onChange={(e) => setNote(e.target.value)} />
                </Field>
              </>
            )}
          </div>
          <div className="space-y-2">
            <p className="text-xs font-medium text-muted-foreground">Policy preview</p>
            {dest === "machine" ? (
              preview.data ? (
                <pre className="max-h-80 overflow-auto rounded-lg border border-border/60 bg-muted/20 p-3 font-mono text-[11px] leading-relaxed">{preview.data.acls}</pre>
              ) : (
                <p className="text-xs text-muted-foreground">{preview.error ? preview.error.message : "Choose a source and a machine to see the lines this adds to the mesh's policy."}</p>
              )
            ) : (
              <p className="text-xs text-muted-foreground">A grant reaches the mesh through its owner's machines: see it under By machine once saved.</p>
            )}
          </div>
        </div>
        {save.error && <p role="alert" className="text-xs text-destructive">{save.error.message}</p>}
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>Cancel</Button>
          <Button disabled={!ready || save.isPending} onClick={() => save.mutate()}>
            {save.isPending && <Loader2 className="size-3.5 animate-spin" />}Save rule
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

// ── Editing a grant ───────────────────────────────────────────────────────────

/**
 * A grant's console actions and, for a member, whether their machines connect
 * to it: the same switches as the resource's Access tab, from here.
 */
function GrantEditor({ orgId, token, rule, onClose, onChanged }: {
  orgId: string; token: string; rule: AccessRule | null; onClose: () => void; onChanged: () => void
}) {
  const [actions, setActions] = useState<Set<string>>(new Set())
  const [reach, setReach] = useState<boolean | undefined>(undefined)
  useEffect(() => {
    if (rule) { setActions(new Set(rule.actions ?? [])); setReach(rule.reach) }
  }, [rule])
  const toggle = useMutation({
    mutationFn: async (action: ResourceAction) => {
      const body = { resource_type: rule!.to.kind as never, resource_id: rule!.to.id!, action }
      if (actions.has(action)) await permissionsApi.revoke(orgId, rule!.from.id!, body, token)
      else await permissionsApi.grant(orgId, rule!.from.id!, body, token)
      return action
    },
    onSuccess: (action) => {
      setActions((s) => { const n = new Set(s); if (n.has(action)) n.delete(action); else n.add(action); return n })
      onChanged()
    },
  })
  const setMachines = useMutation({
    mutationFn: (v: boolean) => nodesApi.setMeshReach(orgId, { user_id: rule!.from.id!, resource_type: rule!.to.kind as "service" | "stack" | "project", resource_id: rule!.to.id!, reach: v }, token),
    onSuccess: (_, v) => { setReach(v); onChanged() },
  })
  const meshApplies = rule?.from.member && (rule.to.kind === "service" || rule.to.kind === "stack" || rule.to.kind === "project")
  const reachNow = reach ?? !(rule?.to.database ?? false)
  return (
    <Dialog open={!!rule} onOpenChange={(o) => { if (!o) onClose() }}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>Edit rule</DialogTitle>
          <DialogDescription>{rule?.from.name} → {rule?.to.name}</DialogDescription>
        </DialogHeader>
        <div className="space-y-4">
          <div className="space-y-2">
            <p className="text-xs font-medium text-muted-foreground">In the console</p>
            <div className="flex flex-wrap gap-1.5">
              {RESOURCE_ACTIONS.map((a) => (
                <button key={a} type="button" disabled={toggle.isPending} onClick={() => toggle.mutate(a)}
                  className={`h-7 rounded-md border px-2.5 text-xs transition-colors ${actions.has(a) ? "border-primary/30 bg-primary/15 text-primary" : "border-border/40 text-muted-foreground/60 hover:text-muted-foreground"}`}>
                  {a}
                </button>
              ))}
            </div>
            {actions.size === 0 && <p className="text-xs text-muted-foreground">With no action left, the grant is gone and this rule with it.</p>}
          </div>
          {meshApplies ? (
            <div className="flex items-start gap-3 rounded-lg border border-border/40 bg-muted/10 px-3 py-2.5">
              <div className="min-w-0 flex-1">
                <p className="text-xs font-medium">From their machines</p>
                <p className="text-xs text-muted-foreground">
                  {reachNow ? `Their machines connect to ${rule!.ports.length ? `ports ${rule!.ports.join(", ")}` : "its published ports"}.` : "Console only: their machines do not connect to it."}
                  {reach === undefined && " (the default)"}
                </p>
              </div>
              <Switch aria-label="From their machines" checked={reachNow} disabled={setMachines.isPending} onCheckedChange={(v) => setMachines.mutate(Boolean(v))} />
            </div>
          ) : rule && !rule.from.member ? (
            <p className="text-xs text-muted-foreground">{rule.from.name} is not a member, so they have no machines on the mesh: this grant is the console's alone.</p>
          ) : null}
          {(toggle.error || setMachines.error) && <p role="alert" className="text-xs text-destructive">{(toggle.error ?? setMachines.error)!.message}</p>}
        </div>
        <DialogFooter>
          <Button onClick={onClose}>Done</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

// ── Policy ────────────────────────────────────────────────────────────────────

function Policy({ orgId, token }: { orgId: string; token: string }) {
  const { data, isLoading } = useQuery({ queryKey: ["mesh-access", orgId], queryFn: () => nodesApi.meshAccess(orgId, token) })
  const [copied, setCopied] = useState(false)
  const text = useMemo(() => data?.policy ?? "", [data])
  if (isLoading) return <Loader2 className="size-4 animate-spin text-muted-foreground" />
  return (
    <ResourcePanel
      title="Policy"
      description={data?.enforced ? "What Headscale is given: generated from the rules and the machines' owners. Read only: change the rules instead." : "What Headscale would be given once the policy is enforced. Read only: change the rules instead."}
      action={text ? (
        <Button variant="outline" size="sm" className="h-7 px-3 text-xs" onClick={() => { navigator.clipboard?.writeText(text); setCopied(true); setTimeout(() => setCopied(false), 1500) }}>
          <Copy className="size-3.5" />{copied ? "Copied" : "Copy"}
        </Button>
      ) : undefined}
    >
      {text ? (
        <pre className="max-h-[32rem] overflow-auto rounded-lg border border-border/60 bg-muted/20 p-3 font-mono text-[11px] leading-relaxed">{text}</pre>
      ) : (
        <p className="text-sm text-muted-foreground">Only the server's owner sees the policy: it names every organisation's machines.</p>
      )}
    </ResourcePanel>
  )
}

import { Link } from "@tanstack/react-router"
import { useState } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Loader2, PlugZap, Search, TerminalSquare } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Section } from "@/components/services/form-primitives"
import { CopyValue } from "@/components/domains/dns-records"
import { OptionSelect } from "@/components/layout/option-select"
import { cliLogins, oauth, type CliSession, type OAuthConnection, type OrgCliSession } from "@/lib/api"
import { formatRelativeTime } from "@/lib/utils"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"

// Connected sessions: everything signed in as a person. A CLI, through
// `meshploy auth login`, acts as them everywhere; an AI assistant, connected
// through OAuth, acts in one organisation as them or as an agent they chose.

/** One connected session, whatever its kind, as a list shows it. */
interface Session {
  kind: "cli" | "assistant"
  id: string
  title: string
  detail: string
  /** The person, linked to their page, on lists of several people's. */
  person?: { id: string; name: string }
  created: string
  lastUsed?: string
  ended?: string
  /** Absent when the viewer cannot end it. */
  end?: { label: string; run: () => void; pending: boolean }
  /** Why the viewer cannot end it, when that is worth saying. */
  note?: string
}

function fromAssistant(c: OAuthConnection, view: "self" | "member" | "agent" | "org"): Session {
  const actsAs = c.as_agent ? `as the agent ${c.as_agent}` : view === "self" ? "as you" : "as themselves"
  return {
    kind: "assistant", id: c.id, title: c.client_name || "Unnamed client",
    detail: view === "agent" ? `AI assistant · connected by ${c.approved_by_name}` : `AI assistant · ${actsAs}`,
    person: view === "org" ? { id: c.approved_by, name: c.approved_by_name } : undefined,
    created: c.created_at, lastUsed: c.last_used_at, ended: c.revoked_at,
  }
}

function fromCli(s: CliSession | OrgCliSession, view: "self" | "org" | "member"): Session {
  const org = s as OrgCliSession
  return {
    kind: "cli", id: s.id, title: s.host, detail: "CLI · meshploy auth login",
    person: view === "org" ? { id: org.user_id, name: org.user_name } : undefined,
    created: s.created_at, lastUsed: s.last_used_at,
  }
}

/** Live sessions first, the most recently used first within each. */
function ordered(list: Session[]) {
  const at = (s: Session) => new Date(s.lastUsed ?? s.created).getTime()
  return [...list].sort((a, b) => Number(!!a.ended) - Number(!!b.ended) || at(b) - at(a))
}

function SessionList({ sessions, empty }: { sessions: Session[]; empty: React.ReactNode }) {
  if (sessions.length === 0) return <div className="text-sm text-muted-foreground">{empty}</div>
  return (
    <div className="console-record-list rounded-xl border border-border overflow-hidden divide-y divide-border/40">
      {ordered(sessions).map((s) => (
        <div key={`${s.kind}:${s.id}`} className={`flex items-center gap-3 px-4 py-3 ${s.ended ? "opacity-60" : ""}`}>
          {s.kind === "cli" ? <TerminalSquare className="h-4 w-4 shrink-0 text-muted-foreground" /> : <PlugZap className="h-4 w-4 shrink-0 text-muted-foreground" />}
          <div className="min-w-0 flex-1">
            <p className="truncate text-sm font-medium">
              {s.title}
              {s.person && (
                <> <span className="text-xs font-normal text-muted-foreground">for </span>
                  <Link to="/users/$userId" params={{ userId: s.person.id }} className="text-xs font-normal hover:underline">{s.person.name}</Link></>
              )}
            </p>
            <p className="text-xs text-muted-foreground">
              {s.detail} · signed in {formatRelativeTime(new Date(s.created))}
              {s.lastUsed && <> · last used {formatRelativeTime(new Date(s.lastUsed))}</>}
              {s.ended && <> · disconnected {formatRelativeTime(new Date(s.ended))}</>}
            </p>
          </div>
          {!s.ended && !s.end && s.note && <span className="max-w-[12rem] text-right text-[11px] text-muted-foreground/70">{s.note}</span>}
          {!s.ended && s.end && (
            <Button variant="outline" size="sm" className="h-7 px-3 text-xs" disabled={s.end.pending} onClick={s.end.run}>
              {s.end.pending && <Loader2 className="h-3 w-3 animate-spin" />}
              {s.end.label}
            </Button>
          )}
        </div>
      ))}
    </div>
  )
}

/** Ending an assistant connection, which every list here offers when it may. */
function useDisconnect(orgId: string, token: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => oauth.revoke(orgId, id, token),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["oauth-connections", orgId] })
      qc.invalidateQueries({ queryKey: ["session-counts", orgId] })
    },
  })
}

/**
 * Logging out a CLI from a list of the organisation's: your own always (as
 * yourself), someone else's while they belong to this organisation alone.
 */
function useLogOutCli(orgId: string, token: string) {
  const qc = useQueryClient()
  const me = useAuthStore((s) => s.userId)
  const m = useMutation({
    mutationFn: (s: OrgCliSession) => s.user_id === me ? cliLogins.revoke(s.id, token) : oauth.logOutOrgCli(orgId, s.id, token),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["org-cli-sessions", orgId] })
      qc.invalidateQueries({ queryKey: ["cli-sessions"] })
      qc.invalidateQueries({ queryKey: ["session-counts", orgId] })
    },
  })
  const session = (c: OrgCliSession, view: "org" | "member"): Session => {
    const base = fromCli(c, view)
    if (c.user_id !== me && c.elsewhere) return { ...base, note: "In another organisation too: only they can log it out" }
    return { ...base, end: { label: "Log out", run: () => m.mutate(c), pending: m.isPending && m.variables?.id === c.id } }
  }
  return { session, error: m.error }
}

function withDisconnect(s: Session, d: ReturnType<typeof useDisconnect>): Session {
  return { ...s, end: { label: "Disconnect", run: () => d.mutate(s.id), pending: d.isPending && d.variables === s.id } }
}

/** Meshploy's MCP address as this console serves it, which an assistant connects to. */
export function mcpAddress() {
  return `${window.location.origin}/mcp`
}

/** How to connect an AI assistant: the address, and what happens next. */
export function ConnectAssistantCard() {
  const url = mcpAddress()
  return (
    <div className="space-y-2 rounded-xl border border-primary/25 bg-primary/5 px-4 py-3">
      <p className="flex items-center gap-2 text-sm font-medium"><PlugZap className="h-4 w-4 text-primary" />Connect an AI assistant</p>
      <div className="rounded-lg border border-border/60 bg-background/60 px-3 py-2 text-xs">
        <CopyValue value={url} />
      </div>
      <ol className="list-decimal space-y-0.5 pl-5 text-xs text-muted-foreground">
        <li>In Claude, open Settings, then Connectors, and add a custom connector with this address. In Cursor or VS Code, add it as a remote MCP server.</li>
        <li>The application sends you here to sign in and choose what it may do.</li>
        <li>It then works with your access, apart from server administration, until it is disconnected.</li>
      </ol>
      <p className="text-[11px] text-muted-foreground/70">Claude on the web connects from the internet, so this console has to be reachable from it over HTTPS.</p>
    </div>
  )
}

/** Your own connected sessions, on your account page: each one ended here. */
export function ConnectedSessionsSection() {
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)
  const qc = useQueryClient()
  const clis = useQuery({ queryKey: ["cli-sessions"], queryFn: () => cliLogins.sessions(token) })
  const assistants = useQuery({ queryKey: ["oauth-connections", orgId, "self"], queryFn: () => oauth.connections(orgId!, token), enabled: !!orgId })
  const disconnect = useDisconnect(orgId ?? "", token)
  const logOut = useMutation({
    mutationFn: (id: string) => cliLogins.revoke(id, token),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["cli-sessions"] }),
  })
  const sessions = [
    ...(clis.data ?? []).map((s) => ({ ...fromCli(s, "self"), end: { label: "Log out", run: () => logOut.mutate(s.id), pending: logOut.isPending && logOut.variables === s.id } })),
    ...(assistants.data ?? []).map((c) => withDisconnect(fromAssistant(c, "self"), disconnect)),
  ]
  const error = logOut.error ?? disconnect.error
  return (
    <Section title="Connected sessions" subtitle="Everything signed in as you: CLIs, and AI assistants in this organisation. One unused for 90 days ends by itself.">
      <div className="space-y-4">
        <ConnectAssistantCard />
        {clis.isLoading || assistants.isLoading ? (
          <Loader2 className="h-3.5 w-3.5 animate-spin text-muted-foreground" />
        ) : (
          <SessionList sessions={sessions}
            empty={<>Nothing is signed in as you. Connect an assistant above, or run <code className="font-mono text-xs">meshploy auth login</code> in a terminal.</>} />
        )}
        {error && <p role="alert" className="text-xs text-destructive">{error.message}</p>}
      </div>
    </Section>
  )
}

/** A member's connected sessions, on their page, for an owner or admin. */
export function MemberSessions({ orgId, userId, token }: { orgId: string; userId: string; token: string }) {
  const assistants = useQuery({ queryKey: ["oauth-connections", orgId, "user", userId], queryFn: () => oauth.connections(orgId, token, { userId }) })
  const clis = useQuery({ queryKey: ["org-cli-sessions", orgId, userId], queryFn: () => oauth.orgCliSessions(orgId, token, userId) })
  const disconnect = useDisconnect(orgId, token)
  const logOut = useLogOutCli(orgId, token)
  if (assistants.isLoading || clis.isLoading) return <Loader2 className="h-3.5 w-3.5 animate-spin text-muted-foreground" />
  const sessions = [
    ...(clis.data ?? []).map((s) => logOut.session(s, "member")),
    ...(assistants.data ?? []).map((c) => withDisconnect(fromAssistant(c, "member"), disconnect)),
  ]
  return (
    <div className="space-y-2">
      <SessionList sessions={sessions} empty="None." />
      {(disconnect.error ?? logOut.error) && <p role="alert" className="text-xs text-destructive">{(disconnect.error ?? logOut.error)!.message}</p>}
    </div>
  )
}

/** The assistants acting as an agent, on its page. */
export function AgentSessions({ orgId, agentId, token }: { orgId: string; agentId: string; token: string }) {
  const assistants = useQuery({ queryKey: ["oauth-connections", orgId, "agent", agentId], queryFn: () => oauth.connections(orgId, token, { agentId }) })
  const disconnect = useDisconnect(orgId, token)
  if (assistants.isLoading) return <Loader2 className="h-3.5 w-3.5 animate-spin text-muted-foreground" />
  return <SessionList sessions={(assistants.data ?? []).map((c) => withDisconnect(fromAssistant(c, "agent"), disconnect))} empty="None connected." />
}

type Kind = "all" | "assistant" | "cli"

/** Every connected session in the organisation, on the Access page. */
export function OrgSessions({ orgId, token }: { orgId: string; token: string }) {
  const assistants = useQuery({ queryKey: ["oauth-connections", orgId, "all"], queryFn: () => oauth.connections(orgId, token, { all: true }) })
  const clis = useQuery({ queryKey: ["org-cli-sessions", orgId, "all"], queryFn: () => oauth.orgCliSessions(orgId, token) })
  const disconnect = useDisconnect(orgId, token)
  const logOut = useLogOutCli(orgId, token)
  const [kind, setKind] = useState<Kind>("all")
  const [search, setSearch] = useState("")
  const [showEnded, setShowEnded] = useState(false)
  const sessions = [
    ...(clis.data ?? []).map((s) => logOut.session(s, "org")),
    ...(assistants.data ?? []).map((c) => withDisconnect(fromAssistant(c, "org"), disconnect)),
  ]
  const shown = sessions
    .filter((s) => kind === "all" || s.kind === kind)
    .filter((s) => showEnded || !s.ended)
    .filter((s) => `${s.title} ${s.detail} ${s.person?.name ?? ""}`.toLowerCase().includes(search.toLowerCase()))
  const live = sessions.filter((s) => !s.ended)
  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-3">
        <div className="relative max-w-md flex-1">
          <Search className="absolute left-3 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input aria-label="Search sessions" placeholder="Search by person, client or machine…" value={search} onChange={(e) => setSearch(e.target.value)} className="h-10 pl-8" />
        </div>
        <OptionSelect label="Kind" value={kind} onChange={(v) => setKind(v as Kind)}
          options={[{ value: "all", label: "Every kind" }, { value: "assistant", label: "AI assistants" }, { value: "cli", label: "CLIs" }]} />
        <label className="flex items-center gap-2 text-xs text-muted-foreground">
          <input type="checkbox" checked={showEnded} onChange={(e) => setShowEnded(e.target.checked)} />
          Show disconnected
        </label>
      </div>
      <p className="text-xs text-muted-foreground">
        {live.filter((s) => s.kind === "assistant").length} AI assistants and {live.filter((s) => s.kind === "cli").length} CLIs signed in as this organisation's people.
        {" "}A CLI acts as its person in every organisation they belong to, so one whose person is also in another organisation is logged out only by them.
      </p>
      {assistants.isLoading || clis.isLoading ? (
        <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
      ) : (
        <SessionList sessions={shown} empty={<div className="rounded-xl border border-dashed border-border/60 py-10 text-center">{sessions.length === 0 ? "Nothing is signed in yet." : "No session matches."}</div>} />
      )}
      {(disconnect.error ?? logOut.error) && <p role="alert" className="text-xs text-destructive">{(disconnect.error ?? logOut.error)!.message}</p>}
    </div>
  )
}

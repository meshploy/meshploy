import { Link } from "@tanstack/react-router"
import { useState } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Loader2, Monitor, PlugZap, Search, TerminalSquare } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Section } from "@/components/services/form-primitives"
import { CopyValue } from "@/components/domains/dns-records"
import { OptionSelect } from "@/components/layout/option-select"
import { SegmentedControl } from "@/components/ui/segmented-control"
import { system } from "@/lib/api/system"
import { auth, cliLogins, oauth, type CliSession, type ConsoleSession, type OAuthConnection, type OrgCliSession, type OrgConsoleSession } from "@/lib/api"
import { formatRelativeTime } from "@/lib/utils"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"

// Connected sessions: everything signed in as a person. A browser signed in
// to the console and a CLI, through `meshploy auth login`, act as them
// everywhere; an AI assistant, connected through OAuth, acts in one
// organisation as them or as an agent they chose.

/** One connected session, whatever its kind, as a list shows it. */
interface Session {
  kind: "browser" | "cli" | "assistant"
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

/** A browser and its system from a user agent, in a few words: "Firefox on Linux". */
export function describeAgent(ua: string) {
  const browser = /Edg\//.test(ua) ? "Edge" : /OPR\//.test(ua) ? "Opera" : /Firefox\//.test(ua) ? "Firefox"
    : /Chrome\//.test(ua) ? "Chrome" : /Safari\//.test(ua) ? "Safari" : ""
  const system = /iPhone|iPad/.test(ua) ? "iOS" : /Android/.test(ua) ? "Android" : /Mac OS X|Macintosh/.test(ua) ? "macOS"
    : /Windows/.test(ua) ? "Windows" : /Linux|X11/.test(ua) ? "Linux" : ""
  if (browser && system) return `${browser} on ${system}`
  return browser || system || "Unknown browser"
}

function fromBrowser(s: ConsoleSession | OrgConsoleSession, view: "self" | "org" | "member"): Session {
  const org = s as OrgConsoleSession
  const self = s as ConsoleSession
  return {
    kind: "browser", id: s.id, title: describeAgent(s.user_agent) + (self.current ? " (this browser)" : ""),
    detail: `Console${s.ip ? ` · ${s.ip}` : ""}`,
    person: view === "org" ? { id: org.user_id, name: org.user_name } : undefined,
    created: s.created_at, lastUsed: s.last_seen_at,
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
        <div key={`${s.kind}:${s.id}`} className={`flex flex-wrap items-center gap-3 px-4 py-3 ${s.ended ? "opacity-60" : ""}`}>
          {/* On a list of several people's, whose it is comes first. */}
          {s.person && (
            <Link to="/users/$userId" params={{ userId: s.person.id }} className="group flex w-full min-w-0 items-center gap-2.5 sm:w-44 sm:shrink-0">
              <span className="flex h-7 w-7 shrink-0 items-center justify-center rounded-full bg-primary/10 text-[11px] font-semibold text-primary">
                {s.person.name.slice(0, 2).toUpperCase()}
              </span>
              <span className="truncate text-sm font-medium group-hover:underline">{s.person.name}</span>
            </Link>
          )}
          {s.kind === "browser" ? <Monitor className="h-4 w-4 shrink-0 text-muted-foreground" />
            : s.kind === "cli" ? <TerminalSquare className="h-4 w-4 shrink-0 text-muted-foreground" />
            : <PlugZap className="h-4 w-4 shrink-0 text-muted-foreground" />}
          <div className="min-w-0 flex-1">
            <p className="truncate text-sm font-medium">{s.title}</p>
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
 * Signing out a CLI from a list of the organisation's: your own always (as
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
    if (c.user_id !== me && c.elsewhere) return { ...base, note: "In another organisation too: only they can sign it out" }
    return { ...base, end: { label: "Sign out", run: () => m.mutate(c), pending: m.isPending && m.variables?.id === c.id } }
  }
  return { session, error: m.error }
}

/**
 * Signing a browser out from a list of the organisation's: your own always (as
 * yourself), someone else's while they belong to this organisation alone.
 */
function useEndBrowser(orgId: string, token: string) {
  const qc = useQueryClient()
  const me = useAuthStore((s) => s.userId)
  const m = useMutation({
    mutationFn: (s: OrgConsoleSession) => s.user_id === me ? auth.revokeSession(s.id, token) : oauth.endOrgConsoleSession(orgId, s.id, token),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["org-console-sessions", orgId] })
      qc.invalidateQueries({ queryKey: ["console-sessions"] })
      qc.invalidateQueries({ queryKey: ["session-counts", orgId] })
    },
  })
  const session = (c: OrgConsoleSession, view: "org" | "member"): Session => {
    const base = fromBrowser(c, view)
    if (c.user_id !== me && c.elsewhere) return { ...base, note: "In another organisation too: only they can sign it out" }
    return { ...base, end: { label: "Sign out", run: () => m.mutate(c), pending: m.isPending && m.variables?.id === c.id } }
  }
  return { session, error: m.error }
}

/** Where you are signed in to the console, each one signed out here, on the Account page. */
export function ConsoleSessions() {
  const token = useAuthStore((s) => s.token)!
  const qc = useQueryClient()
  const list = useQuery({ queryKey: ["console-sessions"], queryFn: () => auth.sessions(token) })
  const done = () => qc.invalidateQueries({ queryKey: ["console-sessions"] })
  const end = useMutation({ mutationFn: (id: string) => auth.revokeSession(id, token), onSuccess: done })
  const others = useMutation({ mutationFn: () => auth.revokeOtherSessions(token), onSuccess: done })
  if (list.isLoading) return <Loader2 className="h-3.5 w-3.5 animate-spin text-muted-foreground" />
  const rows = list.data ?? []
  const sessions = rows.map((s) => {
    const base = fromBrowser(s, "self")
    return s.current ? base : { ...base, end: { label: "Sign out", run: () => end.mutate(s.id), pending: end.isPending && end.variables === s.id } }
  })
  const error = end.error ?? others.error
  return (
    <div className="space-y-3">
      <SessionList sessions={sessions} empty="Only this browser." />
      {rows.some((s) => !s.current) && (
        <Button variant="outline" size="sm" className="h-7 text-xs" disabled={others.isPending} onClick={() => others.mutate()}>
          {others.isPending && <Loader2 className="h-3 w-3 animate-spin" />}
          Sign out everywhere else
        </Button>
      )}
      {error && <p role="alert" className="text-xs text-destructive">{error.message}</p>}
    </div>
  )
}

function withDisconnect(s: Session, d: ReturnType<typeof useDisconnect>): Session {
  return { ...s, end: { label: "Disconnect", run: () => d.mutate(s.id), pending: d.isPending && d.variables === s.id } }
}

/** Meshploy's MCP address as this console serves it, which an assistant connects to. */
export function mcpAddress() {
  return `${window.location.origin}/mcp`
}

type Client = "claude-code" | "claude" | "cursor" | "vscode" | "cli"

/** The install link or command for each client, carrying only the address: each one signs in here. */
function clientSetup(client: Client, url: string, edge: boolean): { commands?: string[]; link?: { href: string; label: string }; steps: string[] } {
  const server = { name: "meshploy", type: "http", url }
  switch (client) {
    case "cli":
      return {
        commands: [
          `sudo bash -c "$(curl -fsSL https://meshploy.com/install.sh)" _ --cli-only${edge ? " --edge" : ""}`,
          `meshploy auth login --url ${new URL(url).host}`,
        ],
        steps: [
          `Install the CLI${edge ? ", on this server's edge channel" : ""}. It is released for Linux; on macOS or Windows, build it from source.`,
          "Log in: your browser opens here to approve it, and the terminal acts as you. meshploy mcp then serves the same tools locally.",
        ],
      }
    case "claude-code":
      return {
        commands: [`claude mcp add --transport http meshploy ${url}`],
        steps: ["Run this in a terminal.", "In Claude Code, run /mcp, choose meshploy, then Authenticate: your browser opens here to sign in."],
      }
    case "claude":
      return {
        steps: [
          "In Claude, open Settings, then Connectors, and add a custom connector with the address above.",
          "Click Connect: Claude sends you here to sign in and choose what it may do.",
        ],
      }
    case "cursor":
      return {
        link: { href: `cursor://anysphere.cursor-deeplink/mcp/install?name=meshploy&config=${btoa(JSON.stringify({ url }))}`, label: "Add to Cursor" },
        steps: ["Cursor asks to install the server, then sends you here to sign in the first time it connects."],
      }
    case "vscode":
      return {
        commands: [`code --add-mcp '${JSON.stringify(server)}'`],
        link: { href: `vscode:mcp/install?${encodeURIComponent(JSON.stringify(server))}`, label: "Add to VS Code" },
        steps: ["Use the link, or run the command. VS Code sends you here to sign in the first time it connects."],
      }
  }
}

/** How to connect an AI assistant: the address, and each client's one step to add it. */
export function ConnectionDetails() {
  const url = mcpAddress()
  const [client, setClient] = useState<Client>("claude-code")
  const token = useAuthStore((s) => s.token)
  // The CLI to install is the one on this server's channel.
  const { data: ver } = useQuery({ queryKey: ["system-version"], queryFn: () => system.versionInfo(token!), enabled: !!token, staleTime: 60 * 1000, retry: false })
  const setup = clientSetup(client, url, ver?.channel === "edge")
  return (
    <div className="flex flex-col gap-3">
      <div className="rounded-lg border border-border/60 bg-background/60 px-3 py-2 text-xs">
        <CopyValue value={url} />
      </div>
      <div>
        <SegmentedControl<Client> value={client} onValueChange={setClient}
          options={[{ value: "claude-code", label: "Claude Code" }, { value: "claude", label: "Claude" }, { value: "cursor", label: "Cursor" }, { value: "vscode", label: "VS Code" }, { value: "cli", label: "CLI" }]} />
      </div>
      {(setup.link || setup.commands) && (
        <div className="flex flex-col gap-2">
          {setup.link && (
            <div>
              <a href={setup.link.href} className="inline-flex h-8 items-center gap-1.5 rounded-md border border-primary/30 bg-primary/10 px-3 text-xs font-medium text-primary hover:bg-primary/20">
                <PlugZap className="h-3.5 w-3.5" />{setup.link.label}
              </a>
            </div>
          )}
          {setup.commands?.map((command) => (
            <div key={command} className="rounded-lg border border-border/60 bg-background/60 px-3 py-2 text-xs">
              <CopyValue value={command} />
            </div>
          ))}
        </div>
      )}
      <ol className="list-decimal space-y-1 pl-5 text-xs leading-relaxed text-muted-foreground">
        {setup.steps.map((step) => <li key={step}>{step}</li>)}
        {client !== "cli" && <li>It then works with your access, apart from server administration, until it is disconnected.</li>}
      </ol>
      {client === "claude" && <p className="text-[11px] text-muted-foreground/70">Claude on the web connects from the internet, so this console has to be reachable from it over HTTPS.</p>}
    </div>
  )
}

/** The connection details in a card of their own, for a page that is about something else. */
export function ConnectAssistantCard() {
  return (
    <div className="flex flex-col gap-3 rounded-xl border border-primary/25 bg-primary/5 p-4">
      <p className="flex items-center gap-2 text-sm font-medium"><PlugZap className="h-4 w-4 text-primary" />Connect an AI assistant</p>
      <ConnectionDetails />
    </div>
  )
}

/** Everything signed in as you, each one ended here. */
export function MySessions() {
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
    ...(clis.data ?? []).map((s) => ({ ...fromCli(s, "self"), end: { label: "Sign out", run: () => logOut.mutate(s.id), pending: logOut.isPending && logOut.variables === s.id } })),
    ...(assistants.data ?? []).map((c) => withDisconnect(fromAssistant(c, "self"), disconnect)),
  ]
  const error = logOut.error ?? disconnect.error
  return (
    <div className="space-y-2">
      {clis.isLoading || assistants.isLoading ? (
        <Loader2 className="h-3.5 w-3.5 animate-spin text-muted-foreground" />
      ) : (
        <SessionList sessions={sessions}
          empty={<>Nothing is signed in as you. Connect an assistant above, or run <code className="font-mono text-xs">meshploy auth login</code> in a terminal.</>} />
      )}
      {error && <p role="alert" className="text-xs text-destructive">{error.message}</p>}
    </div>
  )
}

/** Connection details and your sessions together, for a face that has no Connectors page. */
export function ConnectedSessionsSection() {
  return (
    <Section title="Connected sessions" subtitle="Everything signed in as you: CLIs, and AI assistants in this organisation. One unused for 90 days ends by itself.">
      <div className="space-y-4">
        <ConnectAssistantCard />
        <MySessions />
      </div>
    </Section>
  )
}

/** A member's connected sessions, on their page, for an owner or admin. */
export function MemberSessions({ orgId, userId, token }: { orgId: string; userId: string; token: string }) {
  const assistants = useQuery({ queryKey: ["oauth-connections", orgId, "user", userId], queryFn: () => oauth.connections(orgId, token, { userId }) })
  const clis = useQuery({ queryKey: ["org-cli-sessions", orgId, userId], queryFn: () => oauth.orgCliSessions(orgId, token, userId) })
  const browsers = useQuery({ queryKey: ["org-console-sessions", orgId, userId], queryFn: () => oauth.orgConsoleSessions(orgId, token, userId) })
  const disconnect = useDisconnect(orgId, token)
  const logOut = useLogOutCli(orgId, token)
  const signOut = useEndBrowser(orgId, token)
  if (assistants.isLoading || clis.isLoading || browsers.isLoading) return <Loader2 className="h-3.5 w-3.5 animate-spin text-muted-foreground" />
  const sessions = [
    ...(browsers.data ?? []).map((s) => signOut.session(s, "member")),
    ...(clis.data ?? []).map((s) => logOut.session(s, "member")),
    ...(assistants.data ?? []).map((c) => withDisconnect(fromAssistant(c, "member"), disconnect)),
  ]
  return (
    <div className="space-y-2">
      <SessionList sessions={sessions} empty="None." />
      {(disconnect.error ?? logOut.error ?? signOut.error) && <p role="alert" className="text-xs text-destructive">{(disconnect.error ?? logOut.error ?? signOut.error)!.message}</p>}
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

type Kind = "all" | "browser" | "assistant" | "cli"

const count = (n: number, one: string, many: string) => `${n} ${n === 1 ? one : many}`

/** Every connected session in the organisation, on the Access page. */
export function OrgSessions({ orgId, token }: { orgId: string; token: string }) {
  const assistants = useQuery({ queryKey: ["oauth-connections", orgId, "all"], queryFn: () => oauth.connections(orgId, token, { all: true }) })
  const clis = useQuery({ queryKey: ["org-cli-sessions", orgId, "all"], queryFn: () => oauth.orgCliSessions(orgId, token) })
  const browsers = useQuery({ queryKey: ["org-console-sessions", orgId, "all"], queryFn: () => oauth.orgConsoleSessions(orgId, token) })
  const disconnect = useDisconnect(orgId, token)
  const logOut = useLogOutCli(orgId, token)
  const signOut = useEndBrowser(orgId, token)
  const [kind, setKind] = useState<Kind>("all")
  const [search, setSearch] = useState("")
  const [showEnded, setShowEnded] = useState(false)
  const sessions = [
    ...(browsers.data ?? []).map((s) => signOut.session(s, "org")),
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
          options={[{ value: "all", label: "Every kind" }, { value: "browser", label: "Browsers" }, { value: "assistant", label: "AI assistants" }, { value: "cli", label: "CLIs" }]} />
        <label className="flex items-center gap-2 text-xs text-muted-foreground">
          <input type="checkbox" className="h-3.5 w-3.5 accent-primary" checked={showEnded} onChange={(e) => setShowEnded(e.target.checked)} />
          Show disconnected
        </label>
      </div>
      <p className="text-xs text-muted-foreground">
        {count(live.filter((s) => s.kind === "browser").length, "browser", "browsers")}, {count(live.filter((s) => s.kind === "assistant").length, "AI assistant", "AI assistants")} and {count(live.filter((s) => s.kind === "cli").length, "CLI", "CLIs")} signed in as this organisation's people.
        {" "}A browser or a CLI works in every organisation its person belongs to, so one whose person is also in another organisation is signed out only by them.
      </p>
      {assistants.isLoading || clis.isLoading || browsers.isLoading ? (
        <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
      ) : (
        <SessionList sessions={shown} empty={<div className="rounded-xl border border-dashed border-border/60 py-10 text-center">{sessions.length === 0 ? "Nothing is signed in yet." : "No session matches."}</div>} />
      )}
      {(disconnect.error ?? logOut.error ?? signOut.error) && <p role="alert" className="text-xs text-destructive">{(disconnect.error ?? logOut.error ?? signOut.error)!.message}</p>}
    </div>
  )
}

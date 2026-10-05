import { useNavigate } from "@tanstack/react-router"
import { useEffect, useState } from "react"
import { useMutation, useQuery } from "@tanstack/react-query"
import { Bot, Loader2, PlugZap, XCircle } from "lucide-react"
import { agents as agentsApi, ApiError, oauth, orgs as orgsApi, type OAuthRequest } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { Button } from "@/components/ui/button"
import { OptionSelect } from "@/components/layout/option-select"

const card = "rounded-xl border border-border/60 bg-card p-6 space-y-5"

/**
 * Where an MCP client (Claude, an editor) sends someone to let it connect:
 * which organisation, and whether it acts as them or, for an owner or admin,
 * as one of the organisation's agents. `href` is this page's own address, for
 * a sign-in to return to.
 */
export function OAuthApproval({ request, href, signIn }: { request: OAuthRequest; href: string; signIn: string }) {
  const navigate = useNavigate()
  const token = useAuthStore((s) => s.token)!
  const userId = useAuthStore((s) => s.userId)!
  const clearAuth = useAuthStore((s) => s.clearAuth)
  const currentOrg = useOrgStore((s) => s.currentOrg)

  const info = useQuery({ queryKey: ["oauth-request", request.client_id, request.redirect_uri], queryFn: () => oauth.info(request, token), retry: false })
  const orgList = useQuery({ queryKey: ["orgs"], queryFn: () => orgsApi.list(token) })
  const [orgId, setOrgId] = useState<string>("")
  const org = orgId || currentOrg?.id || orgList.data?.[0]?.id || ""
  const role = useQuery({
    queryKey: ["my-org-role", org, userId],
    queryFn: async () => (await orgsApi.listMembers(org, token)).find((m) => m.user_id === userId)?.role ?? "member",
    enabled: !!org,
  })
  const isAdmin = role.data === "owner" || role.data === "admin"
  const agentList = useQuery({ queryKey: ["agents", org], queryFn: () => agentsApi.list(org, token), enabled: !!org && isAdmin })
  const [as, setAs] = useState("")

  const decide = useMutation({
    mutationFn: (approve: boolean) => oauth.decide(request, { approve, org_id: org, agent_id: as || undefined }, token),
    // Back to the client, with the code or with access_denied.
    onSuccess: (r) => { window.location.href = r.redirect },
  })

  // A session that ran out while the tab was open: sign in again and return.
  const failed = info.error ?? decide.error
  const signedOut = failed instanceof ApiError && failed.status === 401
  useEffect(() => {
    if (!signedOut) return
    clearAuth()
    navigate({ href: `${signIn}?next=${encodeURIComponent(href)}` })
  }, [signedOut, clearAuth, navigate, href, signIn])
  if (signedOut) return null

  if (info.isLoading) {
    return (
      <div className={card}>
        <p className="flex items-center justify-center gap-2 text-sm text-muted-foreground">
          <Loader2 className="h-3.5 w-3.5 animate-spin" />
          Checking the request…
        </p>
      </div>
    )
  }
  if (info.error) {
    return (
      <div className={card}>
        <Heading icon={<XCircle className="h-5 w-5 text-muted-foreground" />} title="This connection request is not valid"
          text={`${info.error.message}. Start connecting again from the application.`} />
      </div>
    )
  }
  const name = info.data!.client_name || "An application"
  const agentName = agentList.data?.find((a) => a.id === as)?.name
  return (
    <div className={card}>
      <Heading
        icon={<PlugZap className="h-5 w-5 text-primary" />}
        title={`Connect ${name} to Meshploy`}
        text={<><span className="font-medium text-foreground">{name}</span> at <span className="font-mono text-xs">{info.data!.redirect_host}</span> is asking to use Meshploy's tools.</>}
      />
      {(orgList.data?.length ?? 0) > 1 && (
        <div className="space-y-1.5">
          <p className="text-xs font-medium text-muted-foreground">Organisation</p>
          <OptionSelect label="Organisation" value={org} onChange={(v) => { setOrgId(v); setAs("") }} className="w-full"
            options={(orgList.data ?? []).map((o) => ({ value: o.id, label: o.name }))} />
        </div>
      )}
      {isAdmin && (agentList.data?.length ?? 0) > 0 && (
        <div className="space-y-1.5">
          <p className="text-xs font-medium text-muted-foreground">Acts as</p>
          <OptionSelect label="Acts as" value={as} onChange={setAs} className="w-full"
            options={[{ value: "", label: "You" }, ...(agentList.data ?? []).map((a) => ({ value: a.id, label: `${a.name} (agent)` }))]} />
        </div>
      )}
      <p className="flex items-start gap-2 text-xs text-muted-foreground leading-relaxed">
        <Bot className="mt-0.5 h-3.5 w-3.5 shrink-0" />
        {as
          ? `It will act as the agent ${agentName}, with exactly that agent's access.`
          : "It can do what you can do in this organisation, through Meshploy's tools, apart from server administration."}
        {" "}It stays connected until it is disconnected on the Connectors page, or goes 90 days unused.
      </p>
      {decide.error && (
        <p role="alert" className="text-xs text-destructive bg-destructive/10 border border-destructive/20 rounded-md px-3 py-2">
          {decide.error.message}
        </p>
      )}
      <div className="grid grid-cols-2 gap-2">
        <Button variant="outline" className="h-9" disabled={decide.isPending} onClick={() => decide.mutate(false)}>
          Deny
        </Button>
        <Button className="h-9" disabled={decide.isPending || !org} onClick={() => decide.mutate(true)}>
          {decide.isPending && decide.variables && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
          Allow
        </Button>
      </div>
    </div>
  )
}

function Heading({ icon, title, text }: { icon?: React.ReactNode; title: string; text: React.ReactNode }) {
  return (
    <div className="flex items-start gap-3">
      {icon && <div className="mt-0.5 shrink-0">{icon}</div>}
      <div>
        <h2 className="text-base font-semibold text-foreground">{title}</h2>
        <p className="text-sm text-muted-foreground mt-0.5 leading-relaxed">{text}</p>
      </div>
    </div>
  )
}

import { createFileRoute, Link, useNavigate } from "@tanstack/react-router"
import { useState } from "react"
import { Loader2, Lock, Mail } from "lucide-react"
import { useMutation, useQuery } from "@tanstack/react-query"
import { auth, orgs as orgsApi, ApiError, type ApiInvitationInfo } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { Button } from "@/components/ui/button"
import { PasswordInput } from "@/components/forms/password-input"

export const Route = createFileRoute("/_auth/register")({
  validateSearch: (search): { token?: string } => ({ token: (search.token as string) || undefined }),
  loader: () => auth.status(),
  component: RegisterPage,
})

function RegisterPage() {
  const { registration_open } = Route.useLoaderData()
  const { token: inviteToken } = Route.useSearch()

  const { data: inviteInfo, isLoading: inviteLoading, error: inviteError } = useQuery({
    queryKey: ["invitation", inviteToken],
    queryFn: () => orgsApi.getInvitationByToken(inviteToken!),
    enabled: !!inviteToken,
    retry: false,
  })

  if (inviteToken) {
    if (inviteLoading) {
      return (
        <div className="rounded-xl border border-border/60 bg-card p-6 flex items-center justify-center gap-2 text-muted-foreground text-sm">
          <Loader2 className="h-4 w-4 animate-spin" />
          <span>Loading invitation…</span>
        </div>
      )
    }
    if (inviteError || !inviteInfo) {
      return (
        <div className="rounded-xl border border-border/60 bg-card p-6 space-y-4 text-center">
          <p className="text-sm text-destructive">This invitation link is invalid or has expired.</p>
          <Link to="/login" className="block text-xs text-primary hover:underline underline-offset-4">
            Sign in instead
          </Link>
        </div>
      )
    }
    return <Invitation info={inviteInfo} inviteToken={inviteToken} />
  }

  if (!registration_open) {
    return (
      <div className="rounded-xl border border-border/60 bg-card p-6 space-y-4 text-center">
        <div className="flex items-center justify-center w-10 h-10 rounded-full bg-muted/40 mx-auto">
          <Lock className="h-4 w-4 text-muted-foreground" />
        </div>
        <div>
          <h2 className="text-base font-semibold text-foreground">Registration closed</h2>
          <p className="text-sm text-muted-foreground mt-1">
            This instance already has an owner. Ask them to invite you as a member.
          </p>
        </div>
        <Link to="/login" className="block text-xs text-primary hover:underline underline-offset-4">
          Sign in instead
        </Link>
      </div>
    )
  }

  return <FirstBootRegisterForm />
}

/** The setup token the installer passed in the URL's fragment, if any, removed from the address bar once read. */
function tokenFromFragment(): string {
  const params = new URLSearchParams(window.location.hash.slice(1))
  const token = params.get("setup_token") ?? ""
  if (token) {
    params.delete("setup_token")
    const rest = params.toString()
    window.history.replaceState(window.history.state, "", window.location.pathname + window.location.search + (rest ? `#${rest}` : ""))
  }
  return token
}

function FirstBootRegisterForm() {
  const navigate = useNavigate()
  const setAuth = useAuthStore((s) => s.setAuth)
  const setOrgs = useOrgStore((s) => s.setOrgs)
  const [username, setUsername] = useState("")
  const [email, setEmail] = useState("")
  const [password, setPassword] = useState("")
  // The browser installer links here with the token in the fragment, which no
  // server ever sees; it is read once and taken out of the address bar.
  const [setupToken, setSetupToken] = useState(tokenFromFragment)
  const [error, setError] = useState<string | null>(null)

  const registerMutation = useMutation({
    mutationFn: async () => {
      await auth.register(username, email, password, setupToken.trim())
      const result = await auth.login(email, password)
      const token = result.token!
      const payload = JSON.parse(atob(token.split(".")[1]))
      setAuth(token, payload.uid)
      const orgList = await orgsApi.list(token)
      setOrgs(orgList.map((o) => ({ id: o.id, name: o.name, slug: o.slug })))
    },
    onSuccess: () => navigate({ to: "/" }),
    onError: (err) => setError(err instanceof ApiError ? err.detail : "Something went wrong"),
  })

  return (
    <div className="rounded-xl border border-border/60 bg-card p-6 space-y-5">
      <div>
        <h2 className="text-base font-semibold text-foreground">Create an account</h2>
        <p className="text-sm text-muted-foreground mt-0.5">A default organization is created automatically</p>
      </div>

      <form onSubmit={(e) => { e.preventDefault(); setError(null); registerMutation.mutate() }} className="space-y-4">
        <Field label="Username">
          <input type="text" autoComplete="username" required minLength={3} placeholder="alice"
            value={username} onChange={(e) => setUsername(e.target.value)}
            className="w-full h-9 rounded-md border border-border/60 bg-muted/20 px-3 text-sm text-foreground placeholder:text-muted-foreground/50 focus:outline-none focus:ring-2 focus:ring-ring/50 transition-shadow" />
        </Field>
        <Field label="Email">
          <input type="email" autoComplete="email" required placeholder="you@example.com"
            value={email} onChange={(e) => setEmail(e.target.value)}
            className="w-full h-9 rounded-md border border-border/60 bg-muted/20 px-3 text-sm text-foreground placeholder:text-muted-foreground/50 focus:outline-none focus:ring-2 focus:ring-ring/50 transition-shadow" />
        </Field>
        <PasswordField value={password} onChange={setPassword} />
        <Field label="Setup token">
          <input type="text" placeholder="ms_…" spellCheck={false} autoComplete="off"
            value={setupToken} onChange={(e) => setSetupToken(e.target.value)}
            className="w-full h-9 rounded-md border border-border/60 bg-muted/20 px-3 font-mono text-xs text-foreground placeholder:text-muted-foreground/50 focus:outline-none focus:ring-2 focus:ring-ring/50 transition-shadow" />
          <p className="text-[11px] text-muted-foreground/70 mt-1.5">
            Printed once by the installer. This account will own the instance, so it is
            required unless the server was set up without one. Lost it? Run{" "}
            <code className="font-mono">sudo meshploy setup-token show</code> on the server.
          </p>
        </Field>
        {error && <ErrorBanner>{error}</ErrorBanner>}
        <SubmitButton pending={registerMutation.isPending}>Create account</SubmitButton>
      </form>

      <p className="text-center text-xs text-muted-foreground">
        Already have an account?{" "}
        <Link to="/login" className="text-primary hover:underline underline-offset-4">Sign in</Link>
      </p>
    </div>
  )
}

/**
 * An invitation, answered the way that fits whoever opened it: signed in, they
 * join with that account; with an account but signed out, they sign in and
 * come back here; with none, they create one.
 */
function Invitation({ info, inviteToken }: { info: ApiInvitationInfo; inviteToken: string }) {
  const signedIn = useAuthStore((s) => s.token)
  if (signedIn) return <JoinAsYou info={info} inviteToken={inviteToken} token={signedIn} />
  if (info.account_exists) {
    const back = `/register?token=${encodeURIComponent(inviteToken)}`
    return (
      <InvitationCard info={info}>
        <p className="text-sm text-muted-foreground">
          <span className="text-foreground">{info.email}</span> already has an account here. Sign in with it to join.
        </p>
        <Link to="/login" search={{ next: back }}
          className="inline-flex h-9 w-full items-center justify-center rounded-md bg-primary text-sm font-medium text-primary-foreground hover:bg-primary/90">
          Sign in to join
        </Link>
      </InvitationCard>
    )
  }
  return <InviteRegisterForm info={info} inviteToken={inviteToken} />
}

function InvitationCard({ info, children }: { info: ApiInvitationInfo; children: React.ReactNode }) {
  return (
    <div className="rounded-xl border border-border/60 bg-card p-6 space-y-5">
      <div>
        <h2 className="text-base font-semibold text-foreground">You've been invited</h2>
        <p className="text-sm text-muted-foreground mt-0.5">
          Join <span className="text-foreground font-medium">{info.org_name}</span> as {info.role}
        </p>
      </div>
      {children}
    </div>
  )
}

/** Joining with the account this browser is signed in as. */
function JoinAsYou({ info, inviteToken, token }: { info: ApiInvitationInfo; inviteToken: string; token: string }) {
  const navigate = useNavigate()
  const setOrgs = useOrgStore((s) => s.setOrgs)
  const setCurrentOrg = useOrgStore((s) => s.setCurrentOrg)
  const clearAuth = useAuthStore((s) => s.clearAuth)
  const { data: me } = useQuery({ queryKey: ["me"], queryFn: () => auth.getMe(token) })
  const mine = !me || me.email.toLowerCase() === info.email.toLowerCase()
  const join = useMutation({
    mutationFn: async () => {
      await orgsApi.joinInvitation(inviteToken, token)
      const list = (await orgsApi.list(token)).map((o) => ({ id: o.id, name: o.name, slug: o.slug }))
      setOrgs(list)
      const joined = list.find((o) => o.name === info.org_name)
      if (joined) setCurrentOrg(joined)
    },
    onSuccess: () => navigate({ to: "/" }),
  })
  const useOther = () => {
    auth.logout(token).catch(() => {})
    clearAuth()
  }
  return (
    <InvitationCard info={info}>
      {mine ? (
        <>
          <p className="text-sm text-muted-foreground">
            You are signed in{me ? <> as <span className="text-foreground">{me.email}</span></> : ""}.
          </p>
          {join.error && <ErrorBanner>{join.error instanceof ApiError ? join.error.detail : "Something went wrong"}</ErrorBanner>}
          <form onSubmit={(e) => { e.preventDefault(); join.mutate() }}>
            <SubmitButton pending={join.isPending} disabled={!me}>Join {info.org_name}</SubmitButton>
          </form>
        </>
      ) : (
        <>
          <p className="text-sm text-muted-foreground">
            This invitation is for <span className="text-foreground">{info.email}</span>, and you are signed in as{" "}
            <span className="text-foreground">{me!.email}</span>.
          </p>
          <Button variant="outline" className="w-full" onClick={useOther}>Sign out to use {info.email}</Button>
        </>
      )}
    </InvitationCard>
  )
}

function InviteRegisterForm({ info, inviteToken }: { info: ApiInvitationInfo; inviteToken: string }) {
  const navigate = useNavigate()
  const setAuth = useAuthStore((s) => s.setAuth)
  const setOrgs = useOrgStore((s) => s.setOrgs)
  const [username, setUsername] = useState("")
  const [password, setPassword] = useState("")
  const [error, setError] = useState<string | null>(null)

  const acceptMutation = useMutation({
    mutationFn: async () => {
      await orgsApi.acceptInvitation(inviteToken, username, password)
      const result = await auth.login(info.email, password)
      const token = result.token!
      const payload = JSON.parse(atob(token.split(".")[1]))
      setAuth(token, payload.uid)
      const orgList = await orgsApi.list(token)
      setOrgs(orgList.map((o) => ({ id: o.id, name: o.name, slug: o.slug })))
    },
    onSuccess: () => navigate({ to: "/" }),
    onError: (err) => setError(err instanceof ApiError ? err.detail : "Something went wrong"),
  })

  return (
    <div className="rounded-xl border border-border/60 bg-card p-6 space-y-5">
      <div>
        <h2 className="text-base font-semibold text-foreground">You've been invited</h2>
        <p className="text-sm text-muted-foreground mt-0.5">
          Join <span className="text-foreground font-medium">{info.org_name}</span> as {info.role}
        </p>
      </div>

      <form onSubmit={(e) => { e.preventDefault(); setError(null); acceptMutation.mutate() }} className="space-y-4">
        <Field label="Email">
          <div className="relative">
            <input type="email" readOnly value={info.email}
              className="w-full h-9 rounded-md border border-border/60 bg-muted/40 px-3 pr-9 text-sm text-muted-foreground cursor-not-allowed" />
            <Mail className="absolute right-3 top-1/2 -translate-y-1/2 h-3.5 w-3.5 text-muted-foreground/40 pointer-events-none" />
          </div>
        </Field>
        <Field label="Username">
          <input type="text" autoComplete="username" required minLength={3} placeholder="alice" autoFocus
            value={username} onChange={(e) => setUsername(e.target.value)}
            className="w-full h-9 rounded-md border border-border/60 bg-muted/20 px-3 text-sm text-foreground placeholder:text-muted-foreground/50 focus:outline-none focus:ring-2 focus:ring-ring/50 transition-shadow" />
        </Field>
        <PasswordField value={password} onChange={setPassword} />
        {error && <ErrorBanner>{error}</ErrorBanner>}
        <SubmitButton pending={acceptMutation.isPending} disabled={!username || !password}>
          Create account &amp; join
        </SubmitButton>
      </form>

      <p className="text-center text-xs text-muted-foreground">
        Already have an account?{" "}
        <Link to="/login" search={{ next: `/register?token=${encodeURIComponent(inviteToken)}` }} className="text-primary hover:underline underline-offset-4">Sign in</Link>
      </p>
    </div>
  )
}

function PasswordField({ value, onChange }: { value: string; onChange: (v: string) => void }) {
  return (
    <Field label="Password">
      <PasswordInput autoComplete="new-password" required minLength={8}
        placeholder="Min. 8 characters" value={value} onChange={(e) => onChange(e.target.value)}
        className="w-full h-9 rounded-md border border-border/60 bg-muted/20 px-3 text-sm text-foreground placeholder:text-muted-foreground/50 focus:outline-none focus:ring-2 focus:ring-ring/50 transition-shadow" />
    </Field>
  )
}

function SubmitButton({ pending, disabled, children }: {
  pending: boolean; disabled?: boolean; children: React.ReactNode
}) {
  return (
    <Button type="submit" disabled={pending || disabled}
      className="w-full h-9 rounded-md bg-primary text-primary-foreground text-sm font-medium hover:bg-primary/90 disabled:opacity-60 disabled:cursor-not-allowed transition-colors flex items-center justify-center gap-2">
      {pending && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
      {children}
    </Button>
  )
}

function ErrorBanner({ children }: { children: React.ReactNode }) {
  return (
    <p className="text-xs text-destructive bg-destructive/10 border border-destructive/20 rounded-md px-3 py-2">
      {children}
    </p>
  )
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex flex-col gap-3">
      <label className="text-xs font-medium text-muted-foreground">{label}</label>
      {children}
    </div>
  )
}

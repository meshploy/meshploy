import { createFileRoute, Link, useNavigate } from "@tanstack/react-router"
import { useState } from "react"
import { Eye, EyeOff, Loader2, Lock, Mail } from "lucide-react"
import { useMutation, useQuery } from "@tanstack/react-query"
import { auth, orgs as orgsApi, ApiError, type ApiInvitationInfo } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { Button } from "@/components/ui/button"

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
    return <InviteRegisterForm info={inviteInfo} inviteToken={inviteToken} />
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

function FirstBootRegisterForm() {
  const navigate = useNavigate()
  const setAuth = useAuthStore((s) => s.setAuth)
  const setOrgs = useOrgStore((s) => s.setOrgs)
  const [username, setUsername] = useState("")
  const [email, setEmail] = useState("")
  const [password, setPassword] = useState("")
  const [showPassword, setShowPassword] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const registerMutation = useMutation({
    mutationFn: async () => {
      await auth.register(username, email, password)
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
        <PasswordField value={password} onChange={setPassword} show={showPassword} onToggle={() => setShowPassword((v) => !v)} />
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

function InviteRegisterForm({ info, inviteToken }: { info: ApiInvitationInfo; inviteToken: string }) {
  const navigate = useNavigate()
  const setAuth = useAuthStore((s) => s.setAuth)
  const setOrgs = useOrgStore((s) => s.setOrgs)
  const [username, setUsername] = useState("")
  const [password, setPassword] = useState("")
  const [showPassword, setShowPassword] = useState(false)
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
        <PasswordField value={password} onChange={setPassword} show={showPassword} onToggle={() => setShowPassword((v) => !v)} />
        {error && <ErrorBanner>{error}</ErrorBanner>}
        <SubmitButton pending={acceptMutation.isPending} disabled={!username || !password}>
          Create account &amp; join
        </SubmitButton>
      </form>

      <p className="text-center text-xs text-muted-foreground">
        Already have an account?{" "}
        <Link to="/login" className="text-primary hover:underline underline-offset-4">Sign in</Link>
      </p>
    </div>
  )
}

function PasswordField({ value, onChange, show, onToggle }: {
  value: string; onChange: (v: string) => void; show: boolean; onToggle: () => void
}) {
  return (
    <Field label="Password">
      <div className="relative">
        <input type={show ? "text" : "password"} autoComplete="new-password" required minLength={8}
          placeholder="Min. 8 characters" value={value} onChange={(e) => onChange(e.target.value)}
          className="w-full h-9 rounded-md border border-border/60 bg-muted/20 px-3 pr-9 text-sm text-foreground placeholder:text-muted-foreground/50 focus:outline-none focus:ring-2 focus:ring-ring/50 transition-shadow" />
        <Button type="button" variant="ghost" size="icon-sm" onClick={onToggle}
          className="absolute right-2.5 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground transition-colors">
          {show ? <EyeOff className="h-3.5 w-3.5" /> : <Eye className="h-3.5 w-3.5" />}
        </Button>
      </div>
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
    <div className="space-y-1.5">
      <label className="text-xs font-medium text-muted-foreground">{label}</label>
      {children}
    </div>
  )
}

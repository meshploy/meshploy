import { createFileRoute, Link, useNavigate } from "@tanstack/react-router"
import { useState } from "react"
import { Eye, EyeOff, Loader2 } from "lucide-react"
import { useMutation } from "@tanstack/react-query"
import { auth, orgs, ApiError } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"

export const Route = createFileRoute("/_auth/login")({
  component: LoginPage,
})

function LoginPage() {
  const navigate = useNavigate()
  const setAuth = useAuthStore((s) => s.setAuth)
  const setOrgs = useOrgStore((s) => s.setOrgs)

  const [email, setEmail] = useState("")
  const [password, setPassword] = useState("")
  const [showPassword, setShowPassword] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const loginMutation = useMutation({
    mutationFn: async () => {
      const { token } = await auth.login(email, password)
      // Decode uid from JWT payload (no library needed — just base64)
      const payload = JSON.parse(atob(token.split(".")[1]))
      const userId: string = payload.uid
      setAuth(token, userId)
      // Fetch orgs immediately so the app has org context
      const orgList = await orgs.list(token)
      setOrgs(orgList.map((o) => ({ id: o.id, name: o.name, slug: o.slug })))
    },
    onSuccess: () => navigate({ to: "/nodes" }),
    onError: (err) => {
      setError(err instanceof ApiError ? err.detail : "Something went wrong")
    },
  })

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    setError(null)
    loginMutation.mutate()
  }

  return (
    <div className="rounded-xl border border-border/60 bg-card p-6 space-y-5">
      <div>
        <h2 className="text-base font-semibold text-foreground">Sign in</h2>
        <p className="text-sm text-muted-foreground mt-0.5">Enter your credentials to continue</p>
      </div>

      <form onSubmit={handleSubmit} className="space-y-4">
        <Field label="Email">
          <input
            type="email"
            autoComplete="email"
            required
            placeholder="you@example.com"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            className="w-full h-9 rounded-md border border-border/60 bg-muted/20 px-3 text-sm text-foreground placeholder:text-muted-foreground/50 focus:outline-none focus:ring-2 focus:ring-ring/50 transition-shadow"
          />
        </Field>
        <Field label="Password">
          <div className="relative">
            <input
              type={showPassword ? "text" : "password"}
              autoComplete="current-password"
              required
              placeholder="••••••••"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              className="w-full h-9 rounded-md border border-border/60 bg-muted/20 px-3 pr-9 text-sm text-foreground placeholder:text-muted-foreground/50 focus:outline-none focus:ring-2 focus:ring-ring/50 transition-shadow"
            />
            <button
              type="button"
              onClick={() => setShowPassword((v) => !v)}
              className="absolute right-2.5 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground transition-colors"
            >
              {showPassword ? <EyeOff className="h-3.5 w-3.5" /> : <Eye className="h-3.5 w-3.5" />}
            </button>
          </div>
        </Field>

        {error && (
          <p className="text-xs text-destructive bg-destructive/10 border border-destructive/20 rounded-md px-3 py-2">
            {error}
          </p>
        )}

        <button
          type="submit"
          disabled={loginMutation.isPending}
          className="w-full h-9 rounded-md bg-primary text-primary-foreground text-sm font-medium hover:bg-primary/90 disabled:opacity-60 disabled:cursor-not-allowed transition-colors flex items-center justify-center gap-2"
        >
          {loginMutation.isPending && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
          Sign in
        </button>
      </form>

      <p className="text-center text-xs text-muted-foreground">
        Don&apos;t have an account?{" "}
        <Link to="/register" className="text-primary hover:underline underline-offset-4">
          Create one
        </Link>
      </p>
    </div>
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

import { createFileRoute, Link, useNavigate } from "@tanstack/react-router"
import { useState } from "react"
import { Eye, EyeOff, Loader2, ShieldCheck } from "lucide-react"
import { useMutation } from "@tanstack/react-query"
import { auth, orgs, ApiError } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { Button } from "@/components/ui/button"

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

  // TOTP step state
  const [mfaToken, setMfaToken] = useState<string | null>(null)
  const [totpCode, setTotpCode] = useState("")
  const [trustDevice, setTrustDevice] = useState(true)
  const [useRecovery, setUseRecovery] = useState(false)
  const [recoveryCode, setRecoveryCode] = useState("")

  async function finalize(token: string) {
    const payload = JSON.parse(atob(token.split(".")[1]))
    const userId: string = payload.uid
    setAuth(token, userId)
    const orgList = await orgs.list(token)
    setOrgs(orgList.map((o) => ({ id: o.id, name: o.name, slug: o.slug })))
    navigate({ to: "/" })
  }

  const loginMutation = useMutation({
    mutationFn: () => auth.login(email, password),
    onSuccess: async (result) => {
      if (result.totp_required && result.mfa_token) {
        setMfaToken(result.mfa_token)
        setError(null)
        return
      }
      await finalize(result.token!)
    },
    onError: (err) => setError(err instanceof ApiError ? err.detail : "Something went wrong"),
  })

  const totpMutation = useMutation({
    mutationFn: () => auth.completeTOTPLogin(mfaToken!, totpCode, trustDevice),
    onSuccess: async (result) => { await finalize(result.token) },
    onError: () => setError("Invalid code — try again"),
  })

  const recoveryMutation = useMutation({
    mutationFn: () => auth.completeRecoveryLogin(mfaToken!, recoveryCode),
    onSuccess: async (result) => { await finalize(result.token) },
    onError: () => setError("Invalid or already used recovery code"),
  })

  if (mfaToken) {
    return (
      <div className="rounded-xl border border-border/60 bg-card p-6 space-y-5">
        <div className="flex items-center gap-2.5">
          <ShieldCheck className="h-5 w-5 text-primary" />
          <div>
            <h2 className="text-base font-semibold text-foreground">Two-Factor Authentication</h2>
            <p className="text-sm text-muted-foreground mt-0.5">
              {useRecovery ? "Enter one of your saved recovery codes" : "Enter the code from your authenticator app"}
            </p>
          </div>
        </div>

        <div className="space-y-3">
          {useRecovery ? (
            <input
              type="text"
              autoComplete="off"
              placeholder="xxxxx-xxxxx"
              value={recoveryCode}
              onChange={(e) => setRecoveryCode(e.target.value.toLowerCase())}
              onKeyDown={(e) => { if (e.key === "Enter" && recoveryCode.length > 0) recoveryMutation.mutate() }}
              autoFocus
              className="w-full h-10 rounded-md border border-border/60 bg-muted/20 px-3 text-center text-sm font-mono tracking-widest text-foreground placeholder:text-muted-foreground/40 focus:outline-none focus:ring-2 focus:ring-ring/50 transition-shadow"
            />
          ) : (
            <>
              <input
                type="text"
                inputMode="numeric"
                autoComplete="one-time-code"
                placeholder="000000"
                maxLength={6}
                value={totpCode}
                onChange={(e) => setTotpCode(e.target.value.replace(/\D/g, "").slice(0, 6))}
                onKeyDown={(e) => { if (e.key === "Enter" && totpCode.length === 6) totpMutation.mutate() }}
                autoFocus
                className="w-full h-10 rounded-md border border-border/60 bg-muted/20 px-3 text-center text-lg font-mono tracking-[0.4em] text-foreground placeholder:text-muted-foreground/40 focus:outline-none focus:ring-2 focus:ring-ring/50 transition-shadow"
              />
              <label className="flex items-center gap-2.5 cursor-pointer select-none">
                <input
                  type="checkbox"
                  checked={trustDevice}
                  onChange={(e) => setTrustDevice(e.target.checked)}
                  className="h-3.5 w-3.5 rounded accent-primary cursor-pointer"
                />
                <span className="text-xs text-muted-foreground">Trust this device for 30 days</span>
              </label>
            </>
          )}

          {error && (
            <p className="text-xs text-destructive bg-destructive/10 border border-destructive/20 rounded-md px-3 py-2">
              {error}
            </p>
          )}

          <Button
            onClick={() => useRecovery ? recoveryMutation.mutate() : totpMutation.mutate()}
            disabled={useRecovery ? recoveryCode.length === 0 || recoveryMutation.isPending : totpCode.length !== 6 || totpMutation.isPending}
            className="w-full h-9 rounded-md bg-primary text-primary-foreground text-sm font-medium hover:bg-primary/90 disabled:opacity-60 disabled:cursor-not-allowed transition-colors flex items-center justify-center gap-2"
          >
            {(totpMutation.isPending || recoveryMutation.isPending) && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
            Verify
          </Button>

          <div className="flex items-center justify-between">
            <Button
              variant="ghost"
              onClick={() => { setUseRecovery((v) => !v); setError(null); setTotpCode(""); setRecoveryCode("") }}
              className="text-xs text-muted-foreground hover:text-foreground transition-colors px-0"
            >
              {useRecovery ? "Use authenticator app instead" : "Use a recovery code instead"}
            </Button>
            <Button
              variant="ghost"
              onClick={() => { setMfaToken(null); setTotpCode(""); setRecoveryCode(""); setUseRecovery(false); setError(null) }}
              className="text-xs text-muted-foreground hover:text-foreground transition-colors px-0"
            >
              Back to login
            </Button>
          </div>
        </div>
      </div>
    )
  }

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
            <Button
              variant="ghost"
              size="icon-sm"
              onClick={() => setShowPassword((v) => !v)}
              className="absolute right-2.5 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground transition-colors"
            >
              {showPassword ? <EyeOff className="h-3.5 w-3.5" /> : <Eye className="h-3.5 w-3.5" />}
            </Button>
          </div>
        </Field>

        {error && (
          <p className="text-xs text-destructive bg-destructive/10 border border-destructive/20 rounded-md px-3 py-2">
            {error}
          </p>
        )}

        <Button
          type="submit"
          disabled={loginMutation.isPending}
          className="w-full h-9 rounded-md bg-primary text-primary-foreground text-sm font-medium hover:bg-primary/90 disabled:opacity-60 disabled:cursor-not-allowed transition-colors flex items-center justify-center gap-2"
        >
          {loginMutation.isPending && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
          Sign in
        </Button>
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

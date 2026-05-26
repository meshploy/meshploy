import { createFileRoute } from "@tanstack/react-router"
import { useEffect, useState } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Copy, Loader2, RefreshCw, ShieldCheck, ShieldOff, X } from "lucide-react"
import QRCode from "qrcode"
import { Button } from "@/components/ui/button"
import { auth as authApi } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { Section, Field, inputCls } from "@/components/services/form-primitives"
import { cn } from "@/lib/utils"

export const Route = createFileRoute("/_app/account/")({
  component: AccountPage,
})

function AccountPage() {
  return (
    <div className="p-6 max-w-2xl space-y-6">
      <div>
        <h1 className="text-xl font-semibold tracking-tight">Account</h1>
        <p className="text-sm text-muted-foreground mt-0.5">Manage your personal profile and security settings</p>
      </div>

      <ProfileSection />
      <PasswordSection />
      <TwoFactorSection />
    </div>
  )
}

function ProfileSection() {
  const token = useAuthStore((s) => s.token)!

  const { data: me, isLoading } = useQuery({
    queryKey: ["me"],
    queryFn: () => authApi.getMe(token),
    staleTime: 5 * 60 * 1000,
  })

  return (
    <Section title="Profile" subtitle="Your identity on this platform">
      {isLoading ? (
        <Loader2 className="h-3.5 w-3.5 animate-spin text-muted-foreground" />
      ) : (
        <div className="grid grid-cols-2 gap-4">
          <Field label="Username">
            <input value={me?.username ?? ""} readOnly className={cn(inputCls, "opacity-60 cursor-default")} />
          </Field>
          <Field label="Email">
            <input value={me?.email ?? ""} readOnly className={cn(inputCls, "opacity-60 cursor-default")} />
          </Field>
        </div>
      )}
    </Section>
  )
}

function PasswordSection() {
  const token = useAuthStore((s) => s.token)!
  const [current, setCurrent] = useState("")
  const [next, setNext] = useState("")
  const [confirm, setConfirm] = useState("")
  const [success, setSuccess] = useState(false)

  const changeMut = useMutation({
    mutationFn: () => authApi.changePassword(current, next, token),
    onSuccess: () => {
      setCurrent("")
      setNext("")
      setConfirm("")
      setSuccess(true)
      setTimeout(() => setSuccess(false), 3000)
    },
  })

  const mismatch = confirm.length > 0 && next !== confirm
  const canSave = current.length > 0 && next.length >= 8 && next === confirm && !changeMut.isPending

  return (
    <Section title="Password" subtitle="Choose a strong password of at least 8 characters">
      <div className="space-y-3">
        <Field label="Current password" required>
          <input
            type="password"
            value={current}
            onChange={(e) => setCurrent(e.target.value)}
            placeholder="••••••••"
            className={inputCls}
            autoComplete="current-password"
          />
        </Field>
        <div className="grid grid-cols-2 gap-3">
          <Field label="New password" required>
            <input
              type="password"
              value={next}
              onChange={(e) => setNext(e.target.value)}
              placeholder="••••••••"
              className={inputCls}
              autoComplete="new-password"
            />
          </Field>
          <Field label="Confirm new password" required>
            <input
              type="password"
              value={confirm}
              onChange={(e) => setConfirm(e.target.value)}
              placeholder="••••••••"
              className={cn(inputCls, mismatch && "border-destructive/60 focus:ring-destructive/30")}
              autoComplete="new-password"
            />
          </Field>
        </div>
        {mismatch && <p className="text-xs text-destructive">Passwords don't match</p>}
        {changeMut.isError && (
          <p className="text-xs text-destructive">{(changeMut.error as Error).message}</p>
        )}
        {success && <p className="text-xs text-emerald-400">Password updated successfully</p>}
        <Button size="sm" disabled={!canSave} onClick={() => changeMut.mutate()} className="gap-1.5">
          {changeMut.isPending && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
          Update password
        </Button>
      </div>
    </Section>
  )
}

function TwoFactorSection() {
  const token = useAuthStore((s) => s.token)!
  const qc = useQueryClient()

  type Step = "idle" | "verify" | "codes" | "disable" | "regenerate"
  const [step, setStep] = useState<Step>("idle")
  const [qrDataUrl, setQrDataUrl] = useState("")
  const [secret, setSecret] = useState("")
  const [code, setCode] = useState("")
  const [recoveryCodes, setRecoveryCodes] = useState<string[]>([])
  const [copied, setCopied] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const { data: me, isLoading } = useQuery({
    queryKey: ["me"],
    queryFn: () => authApi.getMe(token),
    staleTime: 5 * 60 * 1000,
  })

  const setupMut = useMutation({
    mutationFn: () => authApi.setupTOTP(token),
    onSuccess: async (data) => {
      const url = await QRCode.toDataURL(data.otp_url, { width: 180, margin: 1 })
      setQrDataUrl(url)
      setSecret(data.secret)
      setStep("verify")
    },
  })

  const enableMut = useMutation({
    mutationFn: () => authApi.enableTOTP(code, token),
    onSuccess: (data) => {
      qc.invalidateQueries({ queryKey: ["me"] })
      setRecoveryCodes(data.recovery_codes)
      setStep("codes")
    },
    onError: () => setError("Invalid code — try again"),
  })

  const disableMut = useMutation({
    mutationFn: () => authApi.disableTOTP(code, token),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["me"] })
      setStep("idle")
    },
    onError: () => setError("Invalid code — try again"),
  })

  const regenMut = useMutation({
    mutationFn: () => authApi.regenerateRecoveryCodes(code, token),
    onSuccess: (data) => {
      setRecoveryCodes(data.recovery_codes)
      setStep("codes")
    },
    onError: () => setError("Invalid code — try again"),
  })

  useEffect(() => { setCode(""); setError(null) }, [step])

  function copyAll() {
    navigator.clipboard.writeText(recoveryCodes.join("\n"))
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  if (isLoading) return null
  const enabled = me?.totp_enabled ?? false

  return (
    <Section
      title="Two-Factor Authentication"
      subtitle="Add an extra layer of security with a TOTP authenticator app"
    >
      {step === "idle" && (
        <div className="space-y-3">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2.5">
              {enabled ? (
                <>
                  <ShieldCheck className="h-4 w-4 text-emerald-400" />
                  <span className="text-sm text-emerald-400">Enabled</span>
                </>
              ) : (
                <>
                  <ShieldOff className="h-4 w-4 text-muted-foreground/50" />
                  <span className="text-sm text-muted-foreground">Not configured</span>
                </>
              )}
            </div>
            {enabled ? (
              <Button size="sm" variant="outline" className="gap-1.5 h-7 text-xs text-destructive border-destructive/30 hover:bg-destructive/10" onClick={() => setStep("disable")}>
                <X className="h-3 w-3" /> Disable
              </Button>
            ) : (
              <Button size="sm" variant="outline" className="gap-1.5 h-7 text-xs" onClick={() => setupMut.mutate()}>
                {setupMut.isPending ? <Loader2 className="h-3 w-3 animate-spin" /> : <ShieldCheck className="h-3 w-3" />}
                Enable 2FA
              </Button>
            )}
          </div>
          {enabled && (
            <button
              onClick={() => setStep("regenerate")}
              className="text-xs text-muted-foreground/60 hover:text-muted-foreground transition-colors"
            >
              Regenerate recovery codes
            </button>
          )}
        </div>
      )}

      {step === "verify" && (
        <div className="space-y-4">
          <p className="text-xs text-muted-foreground">
            Scan this QR code with your authenticator app (Google Authenticator, Authy, etc.), then enter the 6-digit code to confirm.
          </p>
          <div className="flex gap-5 items-start">
            {qrDataUrl && (
              <img src={qrDataUrl} alt="TOTP QR code" className="rounded-lg border border-border/60 shrink-0" width={116} height={116} />
            )}
            <div className="space-y-2 flex-1 min-w-0">
              <div className="space-y-0.5">
                <p className="text-[10px] text-muted-foreground/60 uppercase tracking-wider">Manual entry key</p>
                <code className="text-xs font-mono text-foreground/80 break-all">{secret}</code>
              </div>
              <div className="flex flex-col gap-1.5 pt-1">
                <label className="text-xs text-muted-foreground/60">Verification code</label>
                <input
                  value={code}
                  onChange={(e) => setCode(e.target.value.replace(/\D/g, "").slice(0, 6))}
                  placeholder="000000"
                  className={cn(inputCls, "font-mono tracking-widest text-center w-32")}
                  maxLength={6}
                  autoFocus
                  onKeyDown={(e) => { if (e.key === "Enter" && code.length === 6) enableMut.mutate() }}
                />
              </div>
            </div>
          </div>
          {error && <p className="text-xs text-destructive">{error}</p>}
          <div className="flex items-center gap-2">
            <Button size="sm" onClick={() => enableMut.mutate()} disabled={code.length !== 6 || enableMut.isPending}>
              {enableMut.isPending && <Loader2 className="h-3.5 w-3.5 animate-spin mr-1.5" />}
              Verify &amp; Enable
            </Button>
            <Button size="sm" variant="ghost" onClick={() => setStep("idle")}>Cancel</Button>
          </div>
        </div>
      )}

      {step === "codes" && (
        <div className="space-y-4">
          <div className="rounded-md border border-amber-500/20 bg-amber-500/5 px-4 py-3">
            <p className="text-xs text-amber-400 font-medium">Save these recovery codes</p>
            <p className="text-xs text-muted-foreground mt-1">
              Store them somewhere safe. Each code can only be used once. These won't be shown again.
            </p>
          </div>
          <div className="grid grid-cols-2 gap-1.5">
            {recoveryCodes.map((c) => (
              <code key={c} className="text-xs font-mono bg-muted/30 border border-border/40 rounded px-2.5 py-1.5 text-foreground/80 tracking-wider">
                {c}
              </code>
            ))}
          </div>
          <div className="flex items-center gap-2">
            <Button size="sm" variant="outline" className="gap-1.5" onClick={copyAll}>
              <Copy className="h-3.5 w-3.5" />
              {copied ? "Copied!" : "Copy all"}
            </Button>
            <Button size="sm" onClick={() => setStep("idle")}>Done</Button>
          </div>
        </div>
      )}

      {step === "disable" && (
        <div className="space-y-3">
          <p className="text-xs text-muted-foreground">Enter your current authenticator code to confirm disabling 2FA.</p>
          <input
            value={code}
            onChange={(e) => setCode(e.target.value.replace(/\D/g, "").slice(0, 6))}
            placeholder="000000"
            className={cn(inputCls, "font-mono tracking-widest text-center w-32")}
            maxLength={6}
            autoFocus
            onKeyDown={(e) => { if (e.key === "Enter" && code.length === 6) disableMut.mutate() }}
          />
          {error && <p className="text-xs text-destructive">{error}</p>}
          <div className="flex items-center gap-2">
            <Button size="sm" variant="destructive" onClick={() => disableMut.mutate()} disabled={code.length !== 6 || disableMut.isPending}>
              {disableMut.isPending && <Loader2 className="h-3.5 w-3.5 animate-spin mr-1.5" />}
              Confirm Disable
            </Button>
            <Button size="sm" variant="ghost" onClick={() => setStep("idle")}>Cancel</Button>
          </div>
        </div>
      )}

      {step === "regenerate" && (
        <div className="space-y-3">
          <p className="text-xs text-muted-foreground">
            Enter your authenticator code to generate a new set of recovery codes. Your existing codes will be invalidated immediately.
          </p>
          <input
            value={code}
            onChange={(e) => setCode(e.target.value.replace(/\D/g, "").slice(0, 6))}
            placeholder="000000"
            className={cn(inputCls, "font-mono tracking-widest text-center w-32")}
            maxLength={6}
            autoFocus
            onKeyDown={(e) => { if (e.key === "Enter" && code.length === 6) regenMut.mutate() }}
          />
          {error && <p className="text-xs text-destructive">{error}</p>}
          <div className="flex items-center gap-2">
            <Button size="sm" className="gap-1.5" onClick={() => regenMut.mutate()} disabled={code.length !== 6 || regenMut.isPending}>
              {regenMut.isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <RefreshCw className="h-3.5 w-3.5" />}
              Regenerate
            </Button>
            <Button size="sm" variant="ghost" onClick={() => setStep("idle")}>Cancel</Button>
          </div>
        </div>
      )}
    </Section>
  )
}

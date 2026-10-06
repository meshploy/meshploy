import { useNavigate } from "@tanstack/react-router"
import { useEffect, useState } from "react"
import { useMutation, useQuery } from "@tanstack/react-query"
import { CheckCircle2, Loader2, TerminalSquare, XCircle } from "lucide-react"
import { ApiError, cliLogins } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { Button } from "@/components/ui/button"

const card = "rounded-xl border border-border/60 bg-card p-6 space-y-5"
const inputCls =
  "w-full h-9 rounded-md border border-border/60 bg-muted/20 px-3 text-sm font-mono uppercase tracking-widest text-foreground placeholder:normal-case placeholder:tracking-normal placeholder:font-sans placeholder:text-muted-foreground/50 focus:outline-none focus:ring-2 focus:ring-ring/50 transition-shadow"

/**
 * Where `meshploy auth login` sends someone: they check the code their
 * terminal shows and approve it, and the CLI is signed in as them. `path` is
 * this page's own address and `signIn` the sign-in page that returns to it
 * (`?next=`), so each face that serves it keeps people on its own pages.
 */
export function CliLoginApproval({ code, path, signIn }: { code?: string; path: string; signIn: string }) {
  return code ? <Approve code={code} path={path} signIn={signIn} /> : <EnterCode path={path} />
}

/** This page's address with a code, for a sign-in to return to. */
export function cliLoginHref(path: string, code?: string) {
  return code ? `${path}?code=${encodeURIComponent(code)}` : path
}

/** Reached without a code: the person types the one their terminal shows. */
function EnterCode({ path }: { path: string }) {
  const navigate = useNavigate()
  const [typed, setTyped] = useState("")
  return (
    <div className={card}>
      <Heading title="Sign in the Meshploy CLI" text="Enter the code your terminal shows." />
      <form
        className="space-y-4"
        onSubmit={(e) => {
          e.preventDefault()
          navigate({ href: cliLoginHref(path, typed.trim()) })
        }}
      >
        <input autoFocus required value={typed} onChange={(e) => setTyped(e.target.value)} placeholder="XXXX-XXXX" aria-label="Code" className={inputCls} />
        <Button type="submit" className="w-full h-9">Continue</Button>
      </form>
    </div>
  )
}

function Approve({ code, path, signIn }: { code: string; path: string; signIn: string }) {
  const navigate = useNavigate()
  const token = useAuthStore((s) => s.token)!
  const clearAuth = useAuthStore((s) => s.clearAuth)
  const login = useQuery({ queryKey: ["cli-login", code], queryFn: () => cliLogins.get(code, token), retry: false })
  const [done, setDone] = useState<"approved" | "denied" | null>(null)
  const decide = useMutation({
    mutationFn: (approve: boolean) => (approve ? cliLogins.approve(code, token) : cliLogins.deny(code, token)),
    onSuccess: (_, approve) => setDone(approve ? "approved" : "denied"),
  })

  // A session that ran out while the tab was open: sign in again and return.
  const failed = login.error ?? decide.error
  const signedOut = failed instanceof ApiError && failed.status === 401
  useEffect(() => {
    if (!signedOut) return
    clearAuth()
    navigate({ href: `${signIn}?next=${encodeURIComponent(cliLoginHref(path, code))}` })
  }, [signedOut, clearAuth, navigate, code, path, signIn])
  if (signedOut) return null

  if (login.isLoading) {
    return (
      <div className={card}>
        <p className="flex items-center justify-center gap-2 text-sm text-muted-foreground">
          <Loader2 className="h-3.5 w-3.5 animate-spin" />
          Looking up the code…
        </p>
      </div>
    )
  }
  if (login.error) {
    const settled = login.error instanceof ApiError && login.error.status === 409
    return (
      <div className={card}>
        <Heading
          icon={<XCircle className="h-5 w-5 text-muted-foreground" />}
          title={settled ? "This code was already used" : "This code has expired"}
          text={settled
            ? "Someone already approved or denied it. If your terminal is still waiting, run meshploy auth login again."
            : "Codes last ten minutes. Run meshploy auth login again for a new one."}
        />
      </div>
    )
  }
  const host = login.data!.host
  if (done) {
    return (
      <div className={card}>
        {done === "approved" ? (
          <Heading
            icon={<CheckCircle2 className="h-5 w-5 text-primary" />}
            title="The CLI is signed in"
            text={`The terminal on ${host} is signed in as you. You can close this tab and go back to it.`}
          />
        ) : (
          <Heading
            icon={<XCircle className="h-5 w-5 text-muted-foreground" />}
            title="Denied"
            text={`The terminal on ${host} was not signed in.`}
          />
        )}
      </div>
    )
  }
  return (
    <div className={card}>
      <Heading
        icon={<TerminalSquare className="h-5 w-5 text-primary" />}
        title="Sign in the Meshploy CLI"
        text={<>A terminal on <span className="font-medium text-foreground">{host}</span> is asking to act as you.</>}
      />
      <div className="rounded-lg border border-border/60 bg-muted/20 px-4 py-3 text-center">
        <p className="text-xs text-muted-foreground">Approve only if your terminal shows this code</p>
        <p className="mt-1 font-mono text-2xl font-semibold tracking-[0.2em] text-foreground">{login.data!.user_code}</p>
      </div>
      <p className="text-xs text-muted-foreground leading-relaxed">
        It can do anything you can do here, until you sign it out on the Connectors page, or it goes 90 days unused.
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
        <Button className="h-9" disabled={decide.isPending} onClick={() => decide.mutate(true)}>
          {decide.isPending && decide.variables && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
          Approve
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

import { createFileRoute, redirect } from "@tanstack/react-router"
import { AlertTriangle, Terminal } from "lucide-react"
import { auth } from "@/lib/api"

/**
 * Shown when a gateway was installed but never finished setup.
 *
 * Deliberately not a redirect to the installer on :9000: that page is served by
 * `meshploy setup serve`, which may not be running, and bouncing someone to a
 * dead port is the lockout this whole feature exists to remove. Tell them what
 * is missing and the one command that fixes it instead.
 */
export const Route = createFileRoute("/setup-required")({
  loader: async () => {
    const status = await auth.status()
    // Nothing to do here once a domain is configured — never strand someone on
    // this page after they have fixed the thing it complains about.
    if (!status.setup_required) throw redirect({ to: "/" })
    return status
  },
  component: SetupRequiredPage,
})

function SetupRequiredPage() {
  return (
    <div className="min-h-screen flex items-center justify-center p-6 bg-background">
      <div className="w-full max-w-lg space-y-5">
        <div className="flex items-start gap-3">
          <AlertTriangle className="h-5 w-5 text-amber-400 shrink-0 mt-0.5" />
          <div>
            <h1 className="text-base font-semibold text-foreground">Setup was never finished</h1>
            <p className="text-sm text-muted-foreground mt-1">
              This gateway has no base domain. Meshploy serves the console, the API and
              every deployed app as subdomains of one, so most of the console cannot work
              until it has one.
            </p>
          </div>
        </div>

        <div className="rounded-lg border border-border/60 bg-card p-4 space-y-3">
          <p className="text-xs font-medium text-muted-foreground uppercase tracking-wider">
            What is blocked
          </p>
          <ul className="text-sm text-muted-foreground space-y-1.5">
            <li>· <span className="text-foreground">Templates</span> — every one needs a subdomain</li>
            <li>· <span className="text-foreground">Public routes</span> — a hostname and a certificate</li>
            <li>· <span className="text-foreground">Worker nodes</span> — they join through <code className="font-mono text-xs">headscale.&lt;domain&gt;</code></li>
          </ul>
        </div>

        <div className="rounded-lg border border-border/60 bg-card p-4 space-y-2">
          <div className="flex items-center gap-2">
            <Terminal className="h-3.5 w-3.5 text-muted-foreground/70" />
            <p className="text-xs font-medium text-muted-foreground uppercase tracking-wider">
              Finish it from the gateway
            </p>
          </div>
          <pre className="text-xs font-mono bg-muted/30 border border-border/40 rounded-md p-3 overflow-x-auto">
sudo meshploy setup serve</pre>
          <p className="text-[11px] text-muted-foreground/70">
            Serves the setup page on port 9000 and opens it in the firewall while it runs.
            Sign in with the setup token from <code className="font-mono">/opt/meshploy/.env</code>.
          </p>
        </div>

        <p className="text-xs text-muted-foreground/70">
          Already fixed it? <a href="/" className="text-primary hover:underline underline-offset-4">Reload the console</a>.
        </p>
      </div>
    </div>
  )
}

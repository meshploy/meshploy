import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Loader2, TerminalSquare } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Section } from "@/components/services/form-primitives"
import { cliLogins } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { formatRelativeTime } from "@/lib/utils"

/** The CLIs signed in as you through `meshploy auth login`, each one ended here. */
export function CliSessionsSection() {
  const token = useAuthStore((s) => s.token)!
  const qc = useQueryClient()
  const { data: sessions, isLoading } = useQuery({ queryKey: ["cli-sessions"], queryFn: () => cliLogins.sessions(token) })
  const revoke = useMutation({
    mutationFn: (id: string) => cliLogins.revoke(id, token),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["cli-sessions"] }),
  })
  return (
    <Section title="CLI sessions" subtitle="Terminals signed in as you with meshploy auth login. One unused for 90 days signs out by itself.">
      {isLoading ? (
        <Loader2 className="h-3.5 w-3.5 animate-spin text-muted-foreground" />
      ) : !sessions?.length ? (
        <p className="text-sm text-muted-foreground">No CLI is signed in. Run <code className="font-mono text-xs">meshploy auth login</code> in a terminal to sign one in.</p>
      ) : (
        <div className="console-record-list rounded-xl border border-border overflow-hidden divide-y divide-border/40">
          {sessions.map((s) => (
            <div key={s.id} className="flex items-center gap-3 px-4 py-3">
              <TerminalSquare className="h-4 w-4 shrink-0 text-muted-foreground" />
              <div className="min-w-0 flex-1">
                <p className="truncate text-sm font-medium">{s.host}</p>
                <p className="text-xs text-muted-foreground">
                  Signed in {formatRelativeTime(new Date(s.created_at))}
                  {s.last_used_at && <> · last used {formatRelativeTime(new Date(s.last_used_at))}</>}
                </p>
              </div>
              <Button variant="outline" size="sm" className="h-7 px-3 text-xs" disabled={revoke.isPending && revoke.variables === s.id}
                onClick={() => revoke.mutate(s.id)}>
                {revoke.isPending && revoke.variables === s.id && <Loader2 className="h-3 w-3 animate-spin" />}
                Log out
              </Button>
            </div>
          ))}
        </div>
      )}
      {revoke.error && <p role="alert" className="text-xs text-destructive">{revoke.error.message}</p>}
    </Section>
  )
}

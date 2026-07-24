import { useState } from "react"
import { Check, Copy, ShieldAlert } from "lucide-react"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Button } from "@/components/ui/button"

/**
 * TokenRevealDialog shows a freshly-minted agent token exactly once. Mirrors the
 * one-time secret pattern used for node provisioning tokens on the Cluster page —
 * amber "shown once" warning + copy affordances. Optionally surfaces the remote
 * MCP connect endpoint so the pair can be pasted straight into an agent platform.
 */
export function TokenRevealDialog({
  open,
  onOpenChange,
  token,
  agentName,
  mcpUrl,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  token: string | null
  agentName?: string
  mcpUrl?: string
}) {
  const [copiedField, setCopiedField] = useState<string | null>(null)

  const copy = async (text: string, field: string) => {
    await navigator.clipboard.writeText(text)
    setCopiedField(field)
    setTimeout(() => setCopiedField(null), 2000)
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Agent token</DialogTitle>
          <DialogDescription>
            {agentName
              ? <>New token for <span className="text-foreground font-medium">{agentName}</span>.</>
              : "Store this token somewhere safe."}
          </DialogDescription>
        </DialogHeader>

        {/* One-time warning */}
        <div className="flex items-start gap-2 rounded-md border border-amber-500/20 bg-amber-500/5 px-3 py-2">
          <ShieldAlert className="h-3.5 w-3.5 text-amber-400 shrink-0 mt-0.5" />
          <p className="text-xs text-amber-300/90">
            Shown once — copy it now. You won't be able to see this token again after closing.
          </p>
        </div>

        {/* Token display */}
        <div className="space-y-1.5">
          <p className="text-xs text-muted-foreground font-medium">Token</p>
          <div className="flex items-center gap-2">
            <code className="flex-1 text-xs font-mono bg-muted/50 border border-border/40 rounded px-3 py-2 text-foreground overflow-hidden text-ellipsis whitespace-nowrap">
              {token ?? ""}
            </code>
            <Button
              size="icon"
              variant="ghost"
              className="h-8 w-8 shrink-0"
              onClick={() => token && copy(token, "token")}
            >
              {copiedField === "token" ? <Check className="h-3.5 w-3.5 text-emerald-400" /> : <Copy className="h-3.5 w-3.5" />}
            </Button>
          </div>
        </div>

        {/* MCP connect endpoint */}
        {mcpUrl && (
          <div className="space-y-1.5">
            <p className="text-xs text-muted-foreground font-medium">Remote MCP endpoint</p>
            <div className="flex items-center gap-2">
              <code className="flex-1 text-xs font-mono bg-muted/50 border border-border/40 rounded px-3 py-2 text-foreground overflow-hidden text-ellipsis whitespace-nowrap">
                {mcpUrl}
              </code>
              <Button
                size="icon"
                variant="ghost"
                className="h-8 w-8 shrink-0"
                onClick={() => copy(mcpUrl, "url")}
              >
                {copiedField === "url" ? <Check className="h-3.5 w-3.5 text-emerald-400" /> : <Copy className="h-3.5 w-3.5" />}
              </Button>
            </div>
            <p className="text-[11px] text-muted-foreground/60">
              Paste this URL and the token above into your agent platform to connect over MCP.
            </p>
          </div>
        )}

        <DialogFooter>
          <Button onClick={() => onOpenChange(false)}>Done</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

import { useEffect, useState } from "react"
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Check, ChevronRight, Loader2, RotateCw, Send } from "lucide-react"
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription } from "@/components/ui/dialog"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { SegmentedControl } from "@/components/ui/segmented-control"
import { useNotificationEvents } from "@/components/notifications/event-picker"
import {
  notifications as notificationsApi,
  emailConfig as emailConfigApi,
  auth as authApi,
  DELIVERY_PAGE,
  type ApiNotificationChannel,
  type ApiNotificationDelivery,
} from "@/lib/api"
import { cn, formatRelativeTime } from "@/lib/utils"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"

const ago = (iso: string) => formatRelativeTime(new Date(iso))

/**
 * How a channel's sends are going, on its row. Before this a channel that had
 * failed every send for a day looked exactly like one that worked.
 */
export function DeliveryStatus({ channel, onOpen }: { channel: ApiNotificationChannel; onOpen: () => void }) {
  const last = channel.last_delivery
  if (!last) return <span className="text-xs">Never sent</span>
  const failing = channel.failing_streak > 0
  return (
    <button
      type="button"
      onClick={onOpen}
      className={cn(
        "text-xs transition-colors",
        failing ? "text-destructive hover:text-destructive/80" : "text-primary hover:text-primary/80"
      )}
    >
      {failing
        ? channel.failing_streak === 1 ? `Failed ${ago(last.created_at)}` : `Failing: ${channel.failing_streak} in a row`
        : `Delivered ${ago(last.created_at)}`}
    </button>
  )
}

/** Sends a test at once. A failure opens the log on that attempt, error and all. */
export function TestChannelButton({ channel, onFailed }: {
  channel: ApiNotificationChannel
  onFailed: (delivery: ApiNotificationDelivery) => void
}) {
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)!
  const qc = useQueryClient()
  const [sent, setSent] = useState(false)

  const test = useMutation({
    mutationFn: () => notificationsApi.test(orgId, channel.id, token),
    onSuccess: (d) => {
      qc.invalidateQueries({ queryKey: ["notification-channels", orgId] })
      qc.invalidateQueries({ queryKey: ["notification-deliveries", channel.id] })
      if (d.success) {
        setSent(true)
        setTimeout(() => setSent(false), 2000)
      } else {
        onFailed(d)
      }
    },
  })

  return (
    <Button variant="outline" size="sm" onClick={() => test.mutate()} disabled={test.isPending}>
      {test.isPending ? <Loader2 className="size-3 animate-spin" /> : sent ? <Check className="size-3" /> : <Send className="size-3" />}
      {sent ? "Sent" : "Test"}
    </Button>
  )
}

/** A channel's attempts, newest first, with the error and a retry on each failure. */
export function DeliveryLogDialog({ channel, open, onOpenChange, focus }: {
  channel: ApiNotificationChannel
  open: boolean
  onOpenChange: (open: boolean) => void
  /** An attempt to open expanded, such as a test that just failed. */
  focus?: string
}) {
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)!
  const qc = useQueryClient()
  const [status, setStatus] = useState<"all" | "failed">("all")
  const [expanded, setExpanded] = useState<string | undefined>(focus)
  const { data: catalogue = [] } = useNotificationEvents()

  useEffect(() => { if (open) setExpanded(focus) }, [open, focus])

  const { data, isLoading, hasNextPage, fetchNextPage, isFetchingNextPage } = useInfiniteQuery({
    queryKey: ["notification-deliveries", channel.id, status],
    queryFn: ({ pageParam }) => notificationsApi.deliveries(orgId, channel.id, status, token, pageParam),
    initialPageParam: undefined as string | undefined,
    // A short page is the last one.
    getNextPageParam: (page) => page.length < DELIVERY_PAGE ? undefined : page[page.length - 1].created_at,
    enabled: open && !!orgId,
  })
  const rows = data?.pages.flat() ?? []

  const retry = useMutation({
    mutationFn: (id: string) => notificationsApi.retry(orgId, id, token),
    onSuccess: (d) => {
      qc.invalidateQueries({ queryKey: ["notification-deliveries", channel.id] })
      qc.invalidateQueries({ queryKey: ["notification-channels", orgId] })
      setExpanded(d.id)
    },
  })

  const title = (event: string) =>
    event === "notification.test" ? "Test notification" : catalogue.find((e) => e.event === event)?.title ?? event
  const about = (d: ApiNotificationDelivery) =>
    [d.data.service, d.data.project].filter(Boolean).join(" · ") || d.data.node || ""

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>Deliveries for {channel.name}</DialogTitle>
          <DialogDescription>Newest first. Attempts are kept for 30 days.</DialogDescription>
        </DialogHeader>

        <SegmentedControl
          value={status}
          onValueChange={setStatus}
          options={[{ value: "all", label: "All" }, { value: "failed", label: "Failed" }]}
          className="justify-self-start"
        />

        <div className="max-h-[60vh] overflow-y-auto rounded-md border border-border/60 divide-y divide-border/60">
          {isLoading ? (
            <div className="flex justify-center py-8"><Loader2 className="size-4 animate-spin text-muted-foreground" /></div>
          ) : rows.length === 0 ? (
            <p className="py-8 text-center text-sm text-muted-foreground">
              {status === "failed" ? "No failed deliveries" : "Nothing sent yet"}
            </p>
          ) : rows.map((d) => {
            const isOpen = expanded === d.id
            return (
              <div key={d.id}>
                <button
                  type="button"
                  onClick={() => setExpanded(isOpen ? undefined : d.id)}
                  className="flex w-full items-center gap-3 px-3 py-2.5 text-left text-sm hover:bg-muted/30 transition-colors"
                >
                  <ChevronRight className={cn("size-3.5 shrink-0 text-muted-foreground transition-transform", isOpen && "rotate-90")} />
                  <span className={cn("size-1.5 shrink-0 rounded-full", d.success ? "bg-primary" : "bg-destructive")} />
                  <span className="min-w-0 flex-1 truncate">
                    {title(d.event)}
                    {about(d) && <span className="text-muted-foreground"> · {about(d)}</span>}
                  </span>
                  <span className={cn("shrink-0 text-xs", d.success ? "text-muted-foreground" : "text-destructive")}>
                    {d.success ? "Delivered" : "Failed"}
                  </span>
                  <span className="w-16 shrink-0 text-right text-xs text-muted-foreground tabular-nums">{ago(d.created_at)}</span>
                </button>
                {isOpen && (
                  <div className="space-y-2 px-3 pb-3 pl-10 text-xs text-muted-foreground">
                    <p>
                      {new Date(d.created_at).toLocaleString()}
                      {d.test && " · test"}
                      {d.retry_of && " · retry"}
                    </p>
                    {d.data.detail && <p>{d.data.detail}</p>}
                    {!d.success && (
                      <>
                        <pre className="whitespace-pre-wrap break-all rounded-md bg-muted/30 p-2 font-mono text-destructive">{d.error}</pre>
                        <div className="flex items-center gap-2">
                          <Button variant="outline" size="sm" onClick={() => retry.mutate(d.id)} disabled={retry.isPending}>
                            {retry.isPending && retry.variables === d.id ? <Loader2 className="size-3 animate-spin" /> : <RotateCw className="size-3" />}
                            Retry
                          </Button>
                          {retry.isError && retry.variables === d.id && (
                            <span className="text-destructive">{(retry.error as Error).message}</span>
                          )}
                        </div>
                      </>
                    )}
                  </div>
                )}
              </div>
            )
          })}
          {hasNextPage && (
            <div className="flex justify-center py-2.5">
              <Button variant="ghost" size="sm" onClick={() => fetchNextPage()} disabled={isFetchingNextPage}>
                {isFetchingNextPage && <Loader2 className="size-3 animate-spin" />}
                Load more
              </Button>
            </div>
          )}
        </div>
      </DialogContent>
    </Dialog>
  )
}

/** Sends a test email through the provider, to an address you choose. */
export function TestEmailProviderButton() {
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)!
  const [open, setOpen] = useState(false)
  const [to, setTo] = useState("")
  const { data: me } = useQuery({ queryKey: ["me"], queryFn: () => authApi.getMe(token), staleTime: 5 * 60 * 1000, enabled: open })

  useEffect(() => { if (open && !to && me?.email) setTo(me.email) }, [open, me?.email, to])

  const test = useMutation({ mutationFn: () => emailConfigApi.test(orgId, to.trim(), token) })

  return <>
    <Button variant="outline" size="sm" onClick={() => { test.reset(); setOpen(true) }}>
      <Send className="size-3" />Test
    </Button>
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Send a test email</DialogTitle>
          <DialogDescription>Goes out through this provider now, so you see any error here rather than after a deploy.</DialogDescription>
        </DialogHeader>
        <form className="space-y-3" onSubmit={(e) => { e.preventDefault(); test.mutate() }}>
          <Input type="email" value={to} onChange={(e) => { setTo(e.target.value); test.reset() }} placeholder="you@company.com" autoFocus />
          {test.data?.success && <p className="text-sm text-primary">Sent to {to.trim()}. Check the inbox, and the spam folder.</p>}
          {test.data && !test.data.success && (
            <pre className="whitespace-pre-wrap break-all rounded-md bg-muted/30 p-2 font-mono text-xs text-destructive">{test.data.error}</pre>
          )}
          {test.isError && <p className="text-sm text-destructive">{(test.error as Error).message}</p>}
          <div className="flex justify-end gap-2">
            <Button type="button" variant="ghost" onClick={() => setOpen(false)}>Close</Button>
            <Button type="submit" disabled={test.isPending || !to.includes("@")}>
              {test.isPending && <Loader2 className="size-3.5 animate-spin" />}Send test
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  </>
}

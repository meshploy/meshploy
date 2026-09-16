import { useMemo, useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { Loader2, Search } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { notifications as notificationsApi, type ApiNotificationEvent } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { cn } from "@/lib/utils"

/**
 * The events a channel subscribes to.
 *
 * The list comes from the API rather than a copy kept here: the console used to
 * carry its own, and job.failed was dispatched for months while missing from
 * it, so nobody could ever receive it.
 */

type Preset = "failures" | "failures-recoveries" | "everything" | "nothing"

const PRESETS: { id: Preset; label: string; hint: string }[] = [
  { id: "failures", label: "Failures", hint: "Everything that means something is wrong, plus who joined the organization" },
  { id: "failures-recoveries", label: "Failures and recoveries", hint: "The above, plus the all-clear when something comes back" },
  { id: "everything", label: "Everything", hint: "Every event, including each success" },
  { id: "nothing", label: "Nothing", hint: "Clear the list and pick by hand" },
]

const RECOVERIES = ["service.recovered", "node.online"]

function presetEvents(preset: Preset, all: ApiNotificationEvent[]): string[] {
  switch (preset) {
    case "failures":
      return all.filter((e) => e.recommended).map((e) => e.event)
    case "failures-recoveries":
      return all.filter((e) => e.recommended || RECOVERIES.includes(e.event)).map((e) => e.event)
    case "everything":
      return all.map((e) => e.event)
    case "nothing":
      return []
  }
}

/** The preset a selection matches exactly, if any, so one can read as active. */
function matchingPreset(selected: string[], all: ApiNotificationEvent[]): Preset | null {
  const same = (a: string[], b: string[]) => a.length === b.length && a.every((x) => b.includes(x))
  for (const { id } of PRESETS) {
    if (same(selected, presetEvents(id, all))) return id
  }
  return null
}

/** The default a new channel starts with, once the catalogue has loaded. */
export function defaultEvents(all: ApiNotificationEvent[]): string[] {
  return presetEvents("failures", all)
}

export function useNotificationEvents() {
  const token = useAuthStore((s) => s.token)!
  return useQuery({
    queryKey: ["notification-events"],
    queryFn: () => notificationsApi.events(token),
    staleTime: 60 * 60 * 1000, // a catalogue changes with a release, not a session
  })
}

export function EventPicker({ events, onChange }: { events: string[]; onChange: (events: string[]) => void }) {
  const { data: all = [], isLoading } = useNotificationEvents()
  const [find, setFind] = useState("")

  const groups = useMemo(() => {
    const q = find.trim().toLowerCase()
    const matches = all.filter(
      (e) => !q || e.title.toLowerCase().includes(q) || e.description.toLowerCase().includes(q) || e.event.includes(q)
    )
    const byGroup = new Map<string, ApiNotificationEvent[]>()
    for (const e of matches) byGroup.set(e.group, [...(byGroup.get(e.group) ?? []), e])
    return [...byGroup.entries()]
  }, [all, find])

  const active = matchingPreset(events, all)
  const toggle = (event: string) =>
    onChange(events.includes(event) ? events.filter((e) => e !== event) : [...events, event])

  const toggleGroup = (group: ApiNotificationEvent[]) => {
    const ids = group.map((e) => e.event)
    const allOn = ids.every((id) => events.includes(id))
    onChange(allOn ? events.filter((e) => !ids.includes(e)) : [...new Set([...events, ...ids])])
  }

  if (isLoading) {
    return (
      <div className="flex items-center gap-2 text-muted-foreground py-2">
        <Loader2 className="h-3.5 w-3.5 animate-spin" />
        <span className="text-xs">Loading events…</span>
      </div>
    )
  }

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        {PRESETS.map((p) => (
          <Button
            key={p.id}
            variant="outline"
            title={p.hint}
            onClick={() => onChange(presetEvents(p.id, all))}
            className={cn(
              "h-7 px-2.5 text-xs",
              active === p.id && "border-primary text-primary bg-primary/5"
            )}
          >
            {p.label}
          </Button>
        ))}
        <span className="ml-auto text-xs text-muted-foreground">
          {events.length} of {all.length} selected
        </span>
      </div>

      <div className="flex items-center gap-2 rounded-md border border-border/60 bg-muted/20 px-2.5">
        <Search className="h-3.5 w-3.5 text-muted-foreground shrink-0" />
        <Input
          value={find}
          placeholder="Find an event…"
          onChange={(e) => setFind(e.target.value)}
          className="h-8! border-0! bg-transparent! px-0 text-sm focus-visible:ring-0! focus-visible:outline-none!"
        />
      </div>

      <div className="rounded-lg border border-border/60 divide-y divide-border/40 max-h-[340px] overflow-y-auto">
        {groups.length === 0 && (
          <p className="px-3 py-4 text-xs text-muted-foreground/60">Nothing matches “{find}”.</p>
        )}
        {groups.map(([group, items]) => {
          const ids = items.map((e) => e.event)
          const on = ids.filter((id) => events.includes(id)).length
          return (
            <div key={group} className="p-3 space-y-2">
              <label className="flex items-center gap-2.5 cursor-pointer">
                <input
                  type="checkbox"
                  checked={on === ids.length}
                  ref={(el) => { if (el) el.indeterminate = on > 0 && on < ids.length }}
                  onChange={() => toggleGroup(items)}
                  className="h-3.5 w-3.5 rounded accent-primary"
                />
                <span className="text-xs font-medium text-foreground">{group}</span>
                <span className="text-[11px] text-muted-foreground/60">{on} of {ids.length}</span>
              </label>

              <div className="space-y-2 pl-6">
                {items.map((e) => (
                  <label key={e.event} className="flex items-start gap-2.5 cursor-pointer group">
                    <input
                      type="checkbox"
                      checked={events.includes(e.event)}
                      onChange={() => toggle(e.event)}
                      className="mt-0.5 h-3.5 w-3.5 rounded accent-primary shrink-0"
                    />
                    <span className="min-w-0">
                      <span className="flex flex-wrap items-center gap-2">
                        <span className="text-xs text-foreground">{e.title}</span>
                        <code className="text-[11px] font-mono text-muted-foreground/50">{e.event}</code>
                      </span>
                      <span className="block text-[11px] text-muted-foreground/70">{e.description}</span>
                    </span>
                  </label>
                ))}
              </div>
            </div>
          )
        })}
      </div>
    </div>
  )
}

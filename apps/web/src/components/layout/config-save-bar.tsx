import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useRef,
  useState,
  type ReactNode,
  type Dispatch,
  type SetStateAction,
} from "react"
import { useBlocker } from "@tanstack/react-router"
import { Info, Loader2 } from "lucide-react"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@/components/ui/dialog"

type Entry = {
  name: string
  dirty: boolean
  save: () => Promise<unknown>
  reset: () => void
}
const Context = createContext<{
  register: (get: () => Entry) => () => void
  notify: () => void
} | null>(null)
const equal = (a: unknown, b: unknown) =>
  JSON.stringify(a) === JSON.stringify(b)

/** Keep background query refreshes from replacing an unsaved draft. */
export function useConfigDraft<T>(initial: T) {
  const [value, setValue] = useState(initial)
  const [baseline, setBaseline] = useState(initial)
  const current = useRef({ value, baseline })
  current.current = { value, baseline }
  const sync = useCallback((incoming: T) => {
    if (!equal(current.current.value, current.current.baseline)) return
    current.current = { value: incoming, baseline: incoming }
    setValue(incoming)
    setBaseline(incoming)
  }, [])
  return {
    value,
    setValue: setValue as Dispatch<SetStateAction<T>>,
    sync,
    dirty: !equal(value, baseline),
    reset: () => setValue(baseline),
    commit: (saved: T) => setBaseline(saved),
  }
}

export function useConfigSave<T>(
  name: string,
  draft: ReturnType<typeof useConfigDraft<T>>,
  save: () => Promise<unknown>,
  enabled = true
) {
  const context = useContext(Context)!
  const latest = useRef<Entry>(null!)
  latest.current = {
    name,
    dirty: enabled && draft.dirty,
    reset: draft.reset,
    save: async () => {
      const snapshot = draft.value
      await save()
      draft.commit(snapshot)
    },
  }
  useEffect(() => context.register(() => latest.current), [context])
  useEffect(() => context.notify(), [context, draft.dirty, enabled])
}

export function ConfigSaveBar({ children }: { children: ReactNode }) {
  const entries = useRef(new Set<() => Entry>())
  const [, update] = useState(0)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const [context] = useState(() => ({
    register: (get: () => Entry) => {
      entries.current.add(get)
      return () => {
        entries.current.delete(get)
      }
    },
    notify: () => update((n) => n + 1),
  }))
  const dirty = [...entries.current].map((get) => get()).filter((e) => e.dirty)
  const shouldBlock = useCallback(
    () => busy || [...entries.current].some((get) => get().dirty),
    [busy]
  )
  const blocker = useBlocker({ shouldBlockFn: shouldBlock, withResolver: true })
  useEffect(() => {
    if (!dirty.length && !busy) return
    const warn = (e: BeforeUnloadEvent) => {
      e.preventDefault()
      e.returnValue = ""
    }
    window.addEventListener("beforeunload", warn)
    return () => window.removeEventListener("beforeunload", warn)
  }, [dirty.length, busy])
  async function save() {
    setBusy(true)
    setError("")
    const pending = [...entries.current]
      .map((get) => get())
      .filter((e) => e.dirty)
    const failures: string[] = []
    // Independent API operations can partially succeed; retain only failed drafts.
    for (const entry of pending) {
      try {
        await entry.save()
      } catch (e) {
        failures.push(
          `${entry.name}: ${e instanceof Error ? e.message : "Could not save"}`
        )
      }
    }
    setError(failures.join(" · "))
    setBusy(false)
    context.notify()
  }
  return (
    <Context value={context}>
      <div className="config-draft-page">
        <fieldset disabled={busy} className="min-w-0 border-0 p-0 m-0">
          {children}
        </fieldset>
        {(dirty.length > 0 || busy) && (
          <div className="config-save-position">
            <div
              className="config-save-bar"
              role="region"
              aria-label="Unsaved configuration changes"
            >
              <Info className="size-4 shrink-0 text-primary" />
              <span className="text-sm flex-1" role="status">
                {busy ? "Saving changes…" : "Unsaved changes"}
              </span>
              <Button
                size="sm"
                variant="outline"
                disabled={busy}
                onClick={() => {
                  dirty.forEach((e) => e.reset())
                  setError("")
                }}
              >
                Reset
              </Button>
              <Button size="sm" disabled={busy} onClick={save}>
                {busy && <Loader2 className="size-3 animate-spin" />}Save
              </Button>
              {error && (
                <p role="alert" className="config-save-error">
                  Some changes could not be saved. {error}
                </p>
              )}
            </div>
          </div>
        )}
      </div>
      <Dialog
        open={blocker.status === "blocked"}
        onOpenChange={(open) => {
          if (!open && blocker.status === "blocked") blocker.reset()
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Leave with unsaved changes?</DialogTitle>
            <DialogDescription>
              Your configuration edits have not been saved.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => blocker.status === "blocked" && blocker.reset()}
            >
              Keep editing
            </Button>
            <Button
              variant="destructive"
              disabled={busy}
              onClick={() => blocker.status === "blocked" && blocker.proceed()}
            >
              Discard and leave
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </Context>
  )
}

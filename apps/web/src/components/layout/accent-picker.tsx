import { useState, useRef, useEffect } from "react"
import { Check } from "lucide-react"
import { ACCENT_GROUPS, getAccent } from "@/lib/accents"
import { useAccentStore } from "@/store/accent-store"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"

export function AccentPicker() {
  const { accentId, setAccent } = useAccentStore()
  const current = getAccent(accentId)
  const [open, setOpen] = useState(false)
  const containerRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return
    function onPointerDown(e: PointerEvent) {
      if (containerRef.current && !containerRef.current.contains(e.target as Node)) {
        setOpen(false)
      }
    }
    document.addEventListener("pointerdown", onPointerDown)
    return () => document.removeEventListener("pointerdown", onPointerDown)
  }, [open])

  function handleSelect(id: string) {
    setAccent(id)
    setOpen(false)
  }

  return (
    <div ref={containerRef} className="relative">
      {/* Trigger: small color swatch + chevron */}
      <Button
        variant="ghost"
        onClick={() => setOpen((v) => !v)}
        className="flex items-center gap-1 h-8 px-2 rounded-md border border-border/60 bg-muted/20 hover:bg-muted/40 transition-colors"
        title="Accent theme"
      >
        <span
          className="h-4 w-4 rounded-sm shrink-0"
          style={{ background: current.value }}
        />
        <svg className="h-3 w-3 text-muted-foreground" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2}>
          <path d="m7 10 5 5 5-5" />
        </svg>
      </Button>

      {/* Dropdown panel */}
      {open && (
        <div className="absolute right-0 top-full mt-2 z-50 w-72 rounded-lg border border-border/60 bg-popover shadow-lg shadow-black/30 overflow-hidden">
          <div className="px-4 py-3 border-b border-border/40 flex items-center justify-between">
            <div>
              <p className="text-sm font-medium">Accent theme</p>
              <p className="text-xs text-muted-foreground mt-0.5">Applies across the dashboard.</p>
            </div>
            <span
              className="text-xs px-2 py-0.5 rounded border border-border/60 text-muted-foreground capitalize"
            >
              {current.label}
            </span>
          </div>

          <div className="p-3 space-y-3 max-h-[420px] overflow-y-auto">
            {ACCENT_GROUPS.map((group) => (
              <div key={group.label}>
                <p className="text-[10px] font-medium text-muted-foreground/60 uppercase tracking-wider mb-1.5 px-1">
                  {group.label}
                </p>
                <div className="grid grid-cols-2 gap-0.5">
                  {group.colors.map((color) => {
                    const isSelected = color.id === accentId
                    return (
                      <Button
                        key={color.id}
                        variant="ghost"
                        onClick={() => handleSelect(color.id)}
                        className={cn(
                          "flex items-center gap-2 px-2.5 py-1.5 rounded-md text-sm transition-colors text-left",
                          isSelected
                            ? "bg-muted/60 text-foreground"
                            : "text-muted-foreground hover:text-foreground hover:bg-muted/30"
                        )}
                      >
                        <span
                          className="h-3.5 w-3.5 rounded-full shrink-0 ring-1 ring-black/20"
                          style={{ background: color.value }}
                        />
                        {color.label}
                        {isSelected && <Check className="h-3 w-3 ml-auto shrink-0 text-foreground" />}
                      </Button>
                    )
                  })}
                </div>
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  )
}

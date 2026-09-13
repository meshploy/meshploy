import { useFormSection } from "@/components/layout/form-layout"
import {
  Children,
  useId,
  isValidElement,
  cloneElement,
  type ReactElement,
} from "react"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"

export const inputCls =
  "w-full h-9 rounded-md border border-border/60 bg-muted/20 px-3 text-sm text-foreground placeholder:text-muted-foreground/50 focus:outline-none focus:ring-2 focus:ring-ring/50 transition-shadow"

export function Section({
  title,
  subtitle,
  danger,
  action,
  children,
}: {
  title: string
  subtitle?: string
  danger?: boolean
  action?: React.ReactNode
  children: React.ReactNode
}) {
  const sectionId = useId()
  useFormSection(sectionId, title)
  return (
    <section
      id={sectionId}
      className={cn(
        "console-section space-y-4",
        danger && "console-section-danger"
      )}
    >
      <div
        className={cn(
          "border-b pb-2 flex items-start justify-between gap-2",
          danger ? "border-destructive/30" : "border-border/40"
        )}
      >
        <div>
          <p
            className={cn(
              "text-sm font-medium",
              danger ? "text-destructive" : "text-foreground"
            )}
          >
            {title}
          </p>
          {subtitle && (
            <p className="text-xs text-muted-foreground mt-0.5">{subtitle}</p>
          )}
        </div>
        {action && <div className="shrink-0">{action}</div>}
      </div>
      {children}
    </section>
  )
}

export function Field({
  label,
  required,
  children,
}: {
  label: string
  required?: boolean
  children: React.ReactNode
}) {
  const fieldId = useId()
  const child = Children.toArray(children).find(
    (c) =>
      isValidElement(c) &&
      (typeof c.type === "string"
        ? ["input", "textarea", "select"].includes(c.type)
        : !!(c.props as { onChange?: unknown }).onChange)
  ) as
    | ReactElement<{
        id?: string
        onChange?: unknown
        "aria-labelledby"?: string
      }>
    | undefined
  const isControl = !!child
  const controlId = child?.props.id || fieldId
  return (
    <div
      className="flex flex-col gap-2"
      role={isControl ? undefined : "group"}
      aria-labelledby={isControl ? undefined : `${fieldId}-label`}
    >
      <label
        id={`${fieldId}-label`}
        htmlFor={isControl ? controlId : undefined}
        className="text-xs font-medium text-muted-foreground"
      >
        {label}
        {required && <span className="text-destructive ml-0.5">*</span>}
      </label>
      {Children.map(children, (c) =>
        isValidElement(c) &&
        child &&
        c.type === child.type &&
        c.props === child.props
          ? cloneElement(c as typeof child, { id: controlId })
          : c
      )}
    </div>
  )
}

export function NodeCard({
  label,
  sub,
  selected,
  onClick,
  online,
}: {
  label: string
  sub: string
  selected: boolean
  onClick: () => void
  online?: boolean
}) {
  return (
    <Button
      variant="ghost"
      aria-pressed={selected}
      onClick={onClick}
      className={cn(
        "flex flex-col gap-0.5 rounded-lg border-2 px-3 py-2.5 text-left transition-all min-w-[120px]",
        selected
          ? "border-primary bg-primary/5"
          : "border-border/60 bg-card hover:border-border hover:bg-muted/20"
      )}
    >
      <div className="flex items-center gap-1.5">
        {online && (
          <span className="h-1.5 w-1.5 rounded-full bg-emerald-400 shrink-0" />
        )}
        <span className="text-xs font-medium text-foreground truncate">
          {label}
        </span>
      </div>
      <span className="text-[11px] text-muted-foreground font-mono truncate">
        {sub}
      </span>
    </Button>
  )
}

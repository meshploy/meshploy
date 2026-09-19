"use client"

import { cn } from "@/lib/utils"

interface SegmentedOption<T extends string> {
  value: T
  label: string
  icon?: React.ReactNode
}

interface SegmentedControlProps<T extends string> {
  value: T
  onValueChange: (value: T) => void
  options: SegmentedOption<T>[]
  className?: string
}

export function SegmentedControl<T extends string>({
  value,
  onValueChange,
  options,
  className,
}: SegmentedControlProps<T>) {
  return (
    <div
      className={cn(
        // w-fit, because inline-flex is not enough: as a child of a flex column
        // the default align-self stretches the track to the container's width
        // while the buttons keep their own, leaving empty track to the right.
        // An explicit width beats stretch, and in a flex row the cross axis is
        // vertical so nothing here changes.
        "inline-flex w-fit items-stretch rounded-md border border-border/60 overflow-hidden text-xs",
        className
      )}
    >
      {options.map((opt, i) => (
        <button
          key={opt.value}
          type="button"
          onClick={() => onValueChange(opt.value)}
          className={cn(
            "flex items-center gap-1.5 px-3 py-1.5 transition-colors whitespace-nowrap",
            value === opt.value
              ? "bg-primary/10 text-primary"
              : "text-muted-foreground hover:text-foreground hover:bg-muted/30",
            i > 0 && "border-l border-border/60"
          )}
        >
          {opt.icon}
          {opt.label}
        </button>
      ))}
    </div>
  )
}

import { useState } from "react"
import { templates as templatesApi } from "@/lib/api"
import { cn } from "@/lib/utils"

/**
 * TemplateLogo renders a template's real logo (served by the API icon endpoint),
 * falling back to a tinted tile with the template's initial if the image is
 * missing or fails to load (e.g. offline with no embedded icon).
 */
export function TemplateLogo({
  id,
  name,
  className,
}: {
  id: string
  name: string
  className?: string
}) {
  const [failed, setFailed] = useState(false)

  if (failed) {
    return (
      <div
        className={cn(
          "flex items-center justify-center rounded-lg bg-primary/10 text-primary font-semibold shrink-0",
          className
        )}
      >
        {name.charAt(0).toUpperCase()}
      </div>
    )
  }

  return (
    <div className={cn("flex items-center justify-center rounded-lg bg-muted/40 overflow-hidden shrink-0", className)}>
      <img
        src={templatesApi.iconUrl(id)}
        alt={name}
        className="w-2/3 h-2/3 object-contain"
        loading="lazy"
        onError={() => setFailed(true)}
      />
    </div>
  )
}

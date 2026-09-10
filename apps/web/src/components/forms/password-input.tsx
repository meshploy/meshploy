import { useState, type ComponentProps } from "react"
import { Eye, EyeOff } from "lucide-react"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"

/**
 * A password or secret input with a show/hide toggle inside its right edge.
 *
 * Every field that hides its value uses this, so they all look and behave the
 * same. The toggle is centred with inset-y-0 and my-auto rather than
 * -translate-y-1/2: the Button's pressed style sets its own transform, which
 * would replace the translate and drop the icon by half its height.
 *
 * className styles the input, as it would a plain <input>. The right padding
 * that keeps typed text clear of the toggle is added here.
 */
export function PasswordInput({ className, ...props }: Omit<ComponentProps<"input">, "type">) {
  const [visible, setVisible] = useState(false)
  return (
    <div className="relative w-full">
      {/* block, so the wrapper is exactly the input's height and the toggle
          centres on the input rather than on a taller line box. */}
      <input {...props} type={visible ? "text" : "password"} className={cn("block", className, "pr-9")} />
      <Button
        type="button"
        variant="ghost"
        size="icon-sm"
        onClick={() => setVisible((v) => !v)}
        aria-label={visible ? "Hide value" : "Show value"}
        aria-pressed={visible}
        className="absolute right-1 inset-y-0 my-auto text-muted-foreground hover:text-foreground"
      >
        {visible ? <EyeOff className="h-3.5 w-3.5" /> : <Eye className="h-3.5 w-3.5" />}
      </Button>
    </div>
  )
}

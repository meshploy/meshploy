import { createRootRoute, Outlet } from "@tanstack/react-router"
import { useEffect } from "react"
import { TooltipProvider } from "@/components/ui/tooltip"
import { useAccentStore } from "@/store/accent-store"
import { getAccent, applyAccent } from "@/lib/accents"

export const Route = createRootRoute({
  component: RootLayout,
})

function RootLayout() {
  const accentId = useAccentStore((s) => s.accentId)

  useEffect(() => {
    applyAccent(getAccent(accentId).value)
  }, [accentId])

  return (
    <TooltipProvider>
      <Outlet />
    </TooltipProvider>
  )
}

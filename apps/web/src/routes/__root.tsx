import { createRootRoute, Outlet, redirect } from "@tanstack/react-router"
import { useEffect } from "react"
import { TooltipProvider } from "@/components/ui/tooltip"
import { useAccentStore } from "@/store/accent-store"
import { getAccent, applyAccent } from "@/lib/accents"
import { eeRedirect } from "@/ee"

export const Route = createRootRoute({
  // An edition serving another face on another host keeps its visitors on
  // its own pages; the console's own door serves everything as it is.
  beforeLoad: ({ location }) => {
    const to = eeRedirect(location.href)
    if (to && to !== location.href) throw redirect({ href: to })
  },
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

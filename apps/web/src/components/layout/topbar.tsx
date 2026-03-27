import { useRouterState, Link } from "@tanstack/react-router"
import { ChevronRight, Home } from "lucide-react"
import { UserMenu } from "./user-menu"

const SEGMENT_LABELS: Record<string, string> = {
  projects: "Projects",
  nodes: "Nodes",
  cluster: "Cluster",
  integrations: "Integrations",
  settings: "Settings",
}

function Breadcrumb() {
  const pathname = useRouterState({ select: (s) => s.location.pathname })
  const segments = pathname.split("/").filter(Boolean)

  return (
    <nav className="flex items-center gap-1 text-sm">
      <Link to="/" className="text-muted-foreground hover:text-foreground transition-colors">
        <Home className="h-3.5 w-3.5" />
      </Link>
      {segments.map((segment, i) => {
        const href = "/" + segments.slice(0, i + 1).join("/")
        const label = SEGMENT_LABELS[segment] ?? segment
        const isLast = i === segments.length - 1

        return (
          <span key={href} className="flex items-center gap-1">
            <ChevronRight className="h-3.5 w-3.5 text-muted-foreground/40" />
            {isLast ? (
              <span className="font-medium text-foreground">{label}</span>
            ) : (
              <Link to={href as "/nodes"} className="text-muted-foreground hover:text-foreground transition-colors">
                {label}
              </Link>
            )}
          </span>
        )
      })}
    </nav>
  )
}

export function Topbar() {
  return (
    <header className="flex items-center h-14 px-6 border-b border-border/40 bg-background/80 backdrop-blur-sm shrink-0 sticky top-0 z-10">
      <div className="flex-1">
        <Breadcrumb />
      </div>
      <div className="flex items-center gap-3">
        <UserMenu />
      </div>
    </header>
  )
}

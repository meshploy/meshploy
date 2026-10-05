import { useNavigate } from "@tanstack/react-router"
import { useQuery } from "@tanstack/react-query"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Avatar, AvatarFallback } from "@/components/ui/avatar"
import { BookOpen, ExternalLink, LogOut, User, PlugZap } from "lucide-react"
import { auth } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"

const DOCS_URL = "https://docs.meshploy.com"

// Room enough to read and to hit: the menu is opened for one choice.
const item = "gap-2.5 px-2.5 py-2 text-sm"

function initials(username: string) {
  const parts = username.trim().split(/[\s_-]+/)
  if (parts.length >= 2) return (parts[0][0] + parts[1][0]).toUpperCase()
  return username.slice(0, 2).toUpperCase()
}

export function UserMenu() {
  const navigate = useNavigate()
  const token = useAuthStore((s) => s.token)!
  const clearAuth = useAuthStore((s) => s.clearAuth)
  const resetOrg = useOrgStore((s) => s.reset)

  const { data: me } = useQuery({
    queryKey: ["me"],
    queryFn: () => auth.getMe(token),
    staleTime: 5 * 60 * 1000,
  })

  function signOut() {
    clearAuth()
    resetOrg()
    navigate({ to: "/login" })
  }

  const abbr = me ? initials(me.username) : "…"

  return (
    <DropdownMenu>
      <DropdownMenuTrigger className="rounded-full outline-none ring-offset-background focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2">
        <Avatar className="h-8 w-8 cursor-pointer">
          <AvatarFallback className="bg-primary/20 text-primary text-xs font-semibold">
            {abbr}
          </AvatarFallback>
        </Avatar>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-60 p-1.5">
        <DropdownMenuLabel className="px-2.5 py-2 font-normal">
          <div className="flex flex-col gap-0.5">
            <p className="text-sm font-medium">{me?.username ?? "—"}</p>
            <p className="text-xs text-muted-foreground truncate">{me?.email ?? ""}</p>
          </div>
        </DropdownMenuLabel>
        <DropdownMenuSeparator />
        <DropdownMenuItem
          className={item}
          onClick={() => navigate({ to: "/account" })}
        >
          <User className="h-3.5 w-3.5" />
          Account
        </DropdownMenuItem>
        <DropdownMenuItem
          className={item}
          onClick={() => navigate({ to: "/connectors" })}
        >
          <PlugZap className="h-3.5 w-3.5" />
          Connectors
        </DropdownMenuItem>
        <DropdownMenuItem className={item} render={<a href={DOCS_URL} target="_blank" rel="noopener noreferrer" />}>
          <BookOpen className="h-3.5 w-3.5" />
          Docs
          <ExternalLink className="ml-auto h-3 w-3 text-muted-foreground" />
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem
          className={`${item} text-destructive focus:text-destructive`}
          onClick={signOut}
        >
          <LogOut className="h-3.5 w-3.5" />
          Sign out
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

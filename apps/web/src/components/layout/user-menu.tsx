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
import { LogOut, User } from "lucide-react"
import { auth } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"

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
      <DropdownMenuContent align="end" className="w-[220px]">
        <DropdownMenuLabel className="font-normal">
          <div className="flex flex-col gap-0.5">
            <p className="text-sm font-medium">{me?.username ?? "—"}</p>
            <p className="text-xs text-muted-foreground truncate">{me?.email ?? ""}</p>
          </div>
        </DropdownMenuLabel>
        <DropdownMenuSeparator />
        <DropdownMenuItem
          className="gap-2 text-sm"
          onClick={() => navigate({ to: "/account" })}
        >
          <User className="h-3.5 w-3.5" />
          Account
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem
          className="gap-2 text-sm text-destructive focus:text-destructive"
          onClick={signOut}
        >
          <LogOut className="h-3.5 w-3.5" />
          Sign out
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

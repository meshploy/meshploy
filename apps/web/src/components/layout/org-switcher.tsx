import { ChevronsUpDown, Building2, Check } from "lucide-react"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { cn } from "@/lib/utils"
import { useOrgStore } from "@/store/org-store"

export function OrgSwitcher() {
  const { currentOrg, orgs, setCurrentOrg } = useOrgStore()

  return (
    <DropdownMenu>
      <DropdownMenuTrigger className="flex items-center gap-2 w-full rounded-md px-2 py-1.5 text-sm text-sidebar-foreground/70 hover:text-sidebar-foreground hover:bg-sidebar-accent transition-colors outline-none">
        <div className="flex items-center justify-center w-5 h-5 rounded bg-primary/20 shrink-0">
          <Building2 className="w-3 h-3 text-primary" />
        </div>
        <span className="flex-1 text-left truncate text-xs font-medium">
          {currentOrg?.name ?? "Loading…"}
        </span>
        <ChevronsUpDown className="w-3 h-3 shrink-0 opacity-50" />
      </DropdownMenuTrigger>
      <DropdownMenuContent
        side="right"
        align="end"
        sideOffset={8}
        className="w-[200px]"
      >
        <DropdownMenuLabel className="text-xs text-muted-foreground">
          Organizations
        </DropdownMenuLabel>
        <DropdownMenuSeparator />
        {orgs.map((org) => (
          <DropdownMenuItem
            key={org.id}
            onClick={() => setCurrentOrg(org)}
            className="gap-2 text-sm"
          >
            <div className="flex items-center justify-center w-5 h-5 rounded bg-muted shrink-0">
              <Building2 className="w-3 h-3" />
            </div>
            <span className="flex-1 truncate">{org.name}</span>
            {currentOrg?.id === org.id && (
              <Check className="w-3.5 h-3.5 text-primary" />
            )}
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

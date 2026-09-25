import { createFileRoute, Link, useNavigate } from "@tanstack/react-router"
import { useState } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { ChevronRight, Globe, Loader2, Plus } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import {
  Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle,
} from "@/components/ui/dialog"
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from "@/components/ui/table"
import { DnsModePicker, dnsModeLabel } from "@/components/domains/dns-mode-picker"
import { domains as domainsApi, ApiError } from "@/lib/api"
import type { ApiDomain, ApiDomainRoute, DnsMode } from "@/lib/api/domains"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore, useIsAdmin } from "@/store/org-store"
import { HelpButton } from "@/help/help-button"
import { cn } from "@/lib/utils"

export const Route = createFileRoute("/_app/domains/")({
  component: DomainsPage,
})

/**
 * Every name this server answers to, in two kinds.
 *
 * A base domain is a zone: routes are subdomains of it, public and internal,
 * and it is managed here - its DNS mode, its records, whether it can go. A
 * custom domain is one whole hostname belonging to one route, so it is
 * configured on that route and only listed here, read-only, so this page is the
 * whole picture rather than half of it.
 */
function DomainsPage() {
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)!
  const isAdmin = useIsAdmin()
  const [adding, setAdding] = useState(false)

  const { data: bases = [], isLoading } = useQuery({
    queryKey: ["domains", orgId],
    queryFn: () => domainsApi.list(orgId, token),
    enabled: !!orgId,
  })
  const { data: custom = [] } = useQuery({
    queryKey: ["custom-domains", orgId],
    queryFn: () => domainsApi.custom(orgId, token),
    enabled: !!orgId,
  })

  return (
    <div className="console-page space-y-6">
      <div className="flex items-start justify-between gap-4">
        <div className="min-w-0">
          <h1 className="text-xl font-semibold tracking-tight flex items-center gap-2" aria-labelledby="page-title"><span id="page-title">Domains</span><HelpButton topic="domains" label="How domains work" /></h1>
          <p className="text-sm text-muted-foreground mt-0.5">
            {bases.length} base {bases.length === 1 ? "domain" : "domains"} · {custom.length} custom
          </p>
        </div>
        {isAdmin && (
          <Button size="sm" variant="outline" className="gap-1.5 h-7 text-xs shrink-0" onClick={() => setAdding(true)}>
            <Plus className="h-3.5 w-3.5" />
            Add base domain
          </Button>
        )}
      </div>

      <section className="space-y-2">
        <div>
          <h2 className="text-sm font-medium">Base domains</h2>
          <p className="text-xs text-muted-foreground mt-0.5">
            Zones whose subdomains routes use, public and internal. Managed here.
          </p>
        </div>
        {isLoading ? (
          <div className="flex items-center gap-2 text-muted-foreground text-sm">
            <Loader2 className="h-3.5 w-3.5 animate-spin" />
            <span>Loading…</span>
          </div>
        ) : (
          <BaseDomainTable rows={bases} />
        )}
      </section>

      <section className="space-y-2">
        <div>
          <h2 className="text-sm font-medium">Custom domains</h2>
          <p className="text-xs text-muted-foreground mt-0.5">
            Whole hostnames that each belong to one route, and are configured on it.
          </p>
        </div>
        {custom.length === 0 ? (
          <div className="rounded-xl border border-dashed border-border/60 px-4 py-6 text-xs text-muted-foreground">
            None yet. A custom domain is a whole hostname, like <code className="text-[11px]">shop.example.org</code>,
            added to a single route from that route&apos;s page.
          </div>
        ) : (
          <CustomDomainTable rows={custom} />
        )}
      </section>

      <AddDomainDialog open={adding} onOpenChange={setAdding} orgId={orgId} token={token} />
    </div>
  )
}

const headCls = "px-4 py-2.5 text-[11px] font-medium text-muted-foreground"
const rowCls = "border-b border-border/30 hover:bg-muted/20 cursor-pointer"

function BaseDomainTable({ rows }: { rows: ApiDomain[] }) {
  const navigate = useNavigate()
  const open = (id: string) => navigate({ to: "/domains/$domainId", params: { domainId: id } })
  return (
    <div className="console-data-table rounded-xl border border-border overflow-hidden">
      <Table>
        <TableHeader className="bg-muted/20">
          <TableRow className="border-b border-border/40 hover:bg-transparent">
            <TableHead className={cn(headCls, "w-[50%]")}>Domain</TableHead>
            <TableHead className={headCls}>DNS mode</TableHead>
            <TableHead aria-label="Open" className="w-10" />
          </TableRow>
        </TableHeader>
        <TableBody>
          {rows.map((d) => (
            <TableRow
              key={d.id}
              tabIndex={0}
              className={rowCls}
              onClick={() => open(d.id)}
              onKeyDown={(e) => {
                if (e.key === "Enter") open(d.id)
              }}
            >
              <TableCell className="px-4 py-3">
                <div className="flex flex-wrap items-center gap-2">
                  <Globe className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
                  <Link
                    to="/domains/$domainId"
                    params={{ domainId: d.id }}
                    className="font-mono text-sm text-foreground hover:underline"
                  >
                    {d.base_domain}
                  </Link>
                  {d.is_primary && <Badge variant="secondary" className="text-[10px]">Primary</Badge>}
                  {d.former_primary && <Badge variant="secondary" className="text-[10px]">Former primary</Badge>}
                  {!d.verified && (
                    <Badge variant="outline" className="text-[10px] border-amber-500/40 text-amber-400">
                      Not verified
                    </Badge>
                  )}
                  {d.retiring_at && (
                    <Badge variant="outline" className="text-[10px] border-amber-500/40 text-amber-400">
                      Retiring
                    </Badge>
                  )}
                </div>
              </TableCell>
              <TableCell className="px-4 py-3 text-xs text-muted-foreground">{dnsModeLabel(d.dns_mode)}</TableCell>
              <TableCell className="px-4 py-3 text-right">
                <ChevronRight className="inline h-4 w-4 text-muted-foreground/50" />
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
}

function CustomDomainTable({ rows }: { rows: ApiDomainRoute[] }) {
  const navigate = useNavigate()
  const open = (r: ApiDomainRoute) =>
    navigate({ to: "/projects/$id/routes/$routeId", params: { id: r.project_id, routeId: r.id } })
  return (
    <div className="console-data-table rounded-xl border border-border overflow-hidden">
      <Table>
        <TableHeader className="bg-muted/20">
          <TableRow className="border-b border-border/40 hover:bg-transparent">
            <TableHead className={cn(headCls, "w-[50%]")}>Hostname</TableHead>
            <TableHead className={headCls}>Project</TableHead>
            <TableHead className={headCls}>State</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {rows.map((r) => (
            <TableRow
              key={r.id}
              tabIndex={0}
              className={rowCls}
              onClick={(e) => {
                if (!(e.target as HTMLElement).closest("a")) open(r)
              }}
              onKeyDown={(e) => {
                if (e.key === "Enter") open(r)
              }}
            >
              <TableCell className="px-4 py-3">
                <Link
                  to="/projects/$id/routes/$routeId"
                  params={{ id: r.project_id, routeId: r.id }}
                  className="font-mono text-sm text-foreground hover:underline"
                >
                  {r.hostname}
                </Link>
              </TableCell>
              <TableCell className="px-4 py-3 text-xs text-muted-foreground">{r.project_name}</TableCell>
              <TableCell className="px-4 py-3 text-xs">
                {!r.verified ? (
                  <span className="text-amber-400">Not verified</span>
                ) : !r.published ? (
                  <span className="text-muted-foreground">Paused</span>
                ) : (
                  <span className="text-emerald-400">Serving</span>
                )}
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
}

function AddDomainDialog({
  open,
  onOpenChange,
  orgId,
  token,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  orgId: string
  token: string
}) {
  const qc = useQueryClient()
  const navigate = useNavigate()
  const [name, setName] = useState("")
  const [mode, setMode] = useState<DnsMode>("delegation")

  const create = useMutation({
    mutationFn: () => domainsApi.create(orgId, { base_domain: name.trim(), dns_mode: mode }, token),
    onSuccess: (d) => {
      qc.invalidateQueries({ queryKey: ["domains", orgId] })
      onOpenChange(false)
      setName("")
      // Straight to the records: a new domain does nothing until they exist.
      navigate({ to: "/domains/$domainId", params: { domainId: d.id } })
    },
  })

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>Add a base domain</DialogTitle>
          <DialogDescription>
            Routes can then use its subdomains, public and internal. Nothing is served on it until
            it is verified.
          </DialogDescription>
        </DialogHeader>

        <form
          className="space-y-4"
          onSubmit={(e) => {
            e.preventDefault()
            if (name.trim()) create.mutate()
          }}
        >
          <Input
            aria-label="Domain"
            placeholder="apps.example.com"
            value={name}
            onChange={(e) => setName(e.target.value)}
            className="h-10 font-mono"
            autoFocus
          />
          <DnsModePicker value={mode} onChange={setMode} disabled={create.isPending} />
          {create.error && (
            <p className="text-xs text-destructive">
              {create.error instanceof ApiError ? create.error.message : "Could not add the domain."}
            </p>
          )}
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={!name.trim() || create.isPending}>
              {create.isPending && <Loader2 className="h-3.5 w-3.5 animate-spin mr-1.5" />}
              Add base domain
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

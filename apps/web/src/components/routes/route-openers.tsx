import { Link } from "@tanstack/react-router"
import { useQuery } from "@tanstack/react-query"
import { Info, Loader2 } from "lucide-react"
import { ResourcePanel } from "@/components/layout/resource-workbench"
import { access as accessApi, type RouteOpener } from "@/lib/api"

// Who an internal route answers on the mesh: the same decision the proxy makes
// for each request, once the mesh policy is enforced. Shown to admins, since
// it names the organisation's members.

function Person({ o }: { o: RouteOpener }) {
  const machines = o.machines.length ? o.machines.join(", ") : "no machine on the mesh yet"
  return (
    <li className="text-sm">
      <Link to="/users/$userId" params={{ userId: o.user_id }} className="font-medium hover:underline">{o.name}</Link>
      <p className="text-xs text-muted-foreground">
        {o.via ? (o.reach ? `Through ${o.via}` : `Granted ${o.via}, switched off from their machines`) : o.role === "owner" ? "Owner" : "Admin"}
        {" · "}{machines}
      </p>
    </li>
  )
}

export function RouteOpenersPanel({ orgId, projectId, routeId, token }: { orgId: string; projectId: string; routeId: string; token: string }) {
  const { data, isLoading } = useQuery({
    queryKey: ["route-openers", orgId, routeId],
    queryFn: () => accessApi.routeOpeners(orgId, projectId, routeId, token),
  })
  const members = data?.members ?? []
  const open = members.filter((m) => m.reach)
  const off = members.filter((m) => !m.reach)
  return (
    <ResourcePanel title="Who can open this" description="From their own machines on the mesh. Everyone else gets Not shared with you.">
      {isLoading || !data ? (
        <Loader2 className="size-4 animate-spin text-muted-foreground" />
      ) : (
        <div className="space-y-4">
          {!data.enforced && (
            <p className="flex items-start gap-1.5 text-xs text-muted-foreground">
              <Info className="mt-0.5 size-3 shrink-0" />
              The mesh policy is not enforced yet, so today every machine on the mesh opens it. This is who will once it is.
            </p>
          )}
          <div className="space-y-1.5">
            <p className="text-xs font-medium text-muted-foreground">Members granted what it leads to</p>
            {open.length === 0 ? (
              <p className="text-sm text-muted-foreground">Nobody yet. Grant a member the service or project on the <Link to="/access" className="text-primary hover:underline">Access</Link> page.</p>
            ) : (
              <ul className="space-y-2">{open.map((o) => <Person key={o.user_id} o={o} />)}</ul>
            )}
          </div>
          {off.length > 0 && (
            <div className="space-y-1.5">
              <p className="text-xs font-medium text-muted-foreground">Granted, but not from their machines</p>
              <ul className="space-y-2">{off.map((o) => <Person key={o.user_id} o={o} />)}</ul>
            </div>
          )}
          {data.rules.length > 0 && (
            <div className="space-y-1.5">
              <p className="text-xs font-medium text-muted-foreground">Network rules to the gateway's web port</p>
              <p className="text-sm">{data.rules.join(", ")}</p>
            </div>
          )}
          <div className="space-y-1.5">
            <p className="text-xs font-medium text-muted-foreground">Always</p>
            <ul className="space-y-2">{data.admins.map((o) => <Person key={o.user_id} o={o} />)}</ul>
            <p className="text-xs text-muted-foreground">Owners and admins, the gateway and the cluster's machines.</p>
          </div>
        </div>
      )}
    </ResourcePanel>
  )
}

import { useMemo, useState } from "react"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { useNavigate } from "@tanstack/react-router"
import { Loader2 } from "lucide-react"
import {
  routes as routesApi,
  tcpRoutes as tcpRoutesApi,
  projects as projectsApi,
  domains as domainsApi,
  type ApiEndpoint,
  type ApiProject,
  type ApiDomain,
  type TCPZone,
} from "@/lib/api"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { SegmentedControl } from "@/components/ui/segmented-control"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
// Routing something Meshploy does not run.
//
// The endpoint is a fact about a node and belongs to no project; the route it
// leads to has to live in one, so the project is asked for here and nowhere
// else. Nothing about the container or the process is touched: this writes a
// route that points at an address.

/** lastProjectKey remembers the project a route was last created in, so the
 *  second endpoint on a server is one click rather than another decision. */
const lastProjectKey = "meshploy.discovery.project"

function readLastProject(): string {
  try {
    return localStorage.getItem(lastProjectKey) ?? ""
  } catch {
    return ""
  }
}

function rememberProject(id: string) {
  try {
    localStorage.setItem(lastProjectKey, id)
  } catch {
    // A private window is not a reason to fail creating a route.
  }
}

/** forwardAddress is what the gateway should dial for this endpoint.
 *
 *  A port bound to every interface is reached on loopback: Caddy and the proxy
 *  run on the gateway's own network, so that is the shortest path and the one
 *  that keeps working if the machine's addresses change. */
function forwardAddress(e: ApiEndpoint): string {
  if (e.address === "0.0.0.0" || e.address === "::" || e.address === "") return "127.0.0.1"
  return e.address
}

export function RouteEndpointDialog({
  endpoint,
  nodeName,
  onClose,
}: {
  endpoint: ApiEndpoint | null
  nodeName: string
  onClose: () => void
}) {
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)
  const qc = useQueryClient()
  const navigate = useNavigate()

  // A known protocol that is not HTTP has no hostname to serve: SSH on a
  // subdomain of the base domain is not a thing anybody wants. An endpoint we
  // could not label is still offered both ways - the label is a guess, and a
  // web app on an odd port is exactly what it fails to recognise.
  const httpPossible = !endpoint || endpoint.http || !endpoint.label
  const [kind, setKind] = useState<"http" | "tcp">(endpoint?.http ? "http" : "tcp")
  const [projectId, setProjectId] = useState("")
  const [domainId, setDomainId] = useState("")
  const [subdomain, setSubdomain] = useState("")
  const [hostname, setHostname] = useState("")
  const [gatewayPort, setGatewayPort] = useState(
    endpoint?.suggested_port ? String(endpoint.suggested_port) : ""
  )
  const [zone, setZone] = useState<TCPZone>("public")

  const { data: projects = [] } = useQuery<ApiProject[]>({
    queryKey: ["projects", orgId],
    queryFn: () => projectsApi.list(orgId!, token),
    enabled: !!orgId && !!endpoint,
  })
  const { data: domainList = [] } = useQuery<ApiDomain[]>({
    queryKey: ["domains", orgId],
    queryFn: () => domainsApi.list(orgId!, token),
    enabled: !!orgId && !!endpoint,
  })
  const verified = useMemo(() => domainList.filter((d) => d.verified), [domainList])

  // Defaults, once the lists arrive: the only project, or the last one used.
  const chosenProject =
    projectId ||
    (projects.some((p) => p.id === readLastProject())
      ? readLastProject()
      : projects.length > 0
        ? projects[0].id
        : "")
  const chosenDomain = domainId || (verified.length > 0 ? verified[0].id : "")

  const create = useMutation({
    mutationFn: async () => {
      if (!endpoint) return
      const ip = forwardAddress(endpoint)
      if (httpPossible && kind === "http") {
        const body: Parameters<typeof routesApi.create>[2] = {
          zone: "public",
          targets: [{
            path: "/",
            strip_path: false,
            target_ip: ip,
            port: endpoint.port,
            ...(endpoint.tls ? { target_tls: true } : {}),
          }],
        }
        if (verified.length > 0 && !hostname) {
          body.domain_id = chosenDomain
          body.subdomain = subdomain.trim()
        } else {
          body.hostname = hostname.trim()
        }
        return routesApi.create(orgId!, chosenProject, body, token)
      }
      return tcpRoutesApi.create(
        orgId!,
        chosenProject,
        {
          gateway_port: parseInt(gatewayPort, 10) || 0,
          zone,
          target_ip: ip,
          target_port: endpoint.port,
        },
        token
      )
    },
    onSuccess: () => {
      rememberProject(chosenProject)
      qc.invalidateQueries({ queryKey: ["discovery", orgId] })
      qc.invalidateQueries({ queryKey: ["routes", orgId, chosenProject] })
      qc.invalidateQueries({ queryKey: ["tcp-routes", orgId, chosenProject] })
      onClose()
      navigate({ to: "/projects/$id/routes", params: { id: chosenProject } })
    },
  })

  if (!endpoint) return null

  const usingDomain = verified.length > 0 && !hostname
  const canCreate =
    chosenProject !== "" &&
    (httpPossible && kind === "http"
      ? usingDomain
        ? chosenDomain !== "" && subdomain.trim() !== ""
        : hostname.trim() !== ""
      : zone === "public"
        ? (parseInt(gatewayPort, 10) || 0) > 0
        : true)

  const httpKind = httpPossible && kind === "http"
  const forward = `${forwardAddress(endpoint)}:${endpoint.port}`

  return (
    <Dialog open onOpenChange={(open) => { if (!open && !create.isPending) onClose() }}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Route {endpoint.container?.name ?? endpoint.process ?? `port ${endpoint.port}`}</DialogTitle>
          <DialogDescription>
            Meshploy will forward to <code className="font-mono">{forward}</code> on {nodeName}. Nothing running there is touched.
          </DialogDescription>
        </DialogHeader>

        <div className="flex flex-col gap-4">
          {httpPossible ? (
            <SegmentedControl
              value={kind}
              onValueChange={(v) => setKind(v as "http" | "tcp")}
              options={[
                { value: "http", label: "Hostname (HTTP)" },
                { value: "tcp", label: "Port (TCP)" },
              ]}
            />
          ) : (
            <p className="text-xs text-muted-foreground">
              {endpoint.label} is not HTTP, so this is a TCP route: the gateway publishes a port rather than a hostname.
            </p>
          )}

          <Field label="Project" hint="Where the route is kept. The endpoint itself belongs to no project.">
            <Select value={chosenProject} onValueChange={(v) => setProjectId(v ?? "")}>
              <SelectTrigger className="w-full data-[size=default]:h-9 text-sm bg-muted/20 border-border/60">
                <SelectValue placeholder="Select a project…">
                  {projects.find((p) => p.id === chosenProject)?.name}
                </SelectValue>
              </SelectTrigger>
              <SelectContent>
                {projects.map((p) => (
                  <SelectItem key={p.id} value={p.id}>{p.name}</SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>

          {httpKind && endpoint.tls && (
            <p className="text-xs text-muted-foreground">
              It speaks HTTPS, so Meshploy terminates the certificate at the edge and re-encrypts to it. Its own certificate is not verified: it is reached by address, over loopback or the mesh.
            </p>
          )}

          {httpKind ? (
            usingDomain ? (
              // One domain needs no picker: the name it will answer on is worth
              // more on screen than a dropdown with a single row in it.
              verified.length === 1 ? (
                <Field label="Subdomain" hint={`It will answer on ${subdomain.trim() || "<subdomain>"}.${verified[0].base_domain}`}>
                  <Input
                    value={subdomain}
                    onChange={(e) => setSubdomain(e.target.value)}
                    placeholder="grafana"
                    className="h-9 font-mono text-sm"
                  />
                </Field>
              ) : (
              <div className="grid grid-cols-[1fr_1.2fr] gap-3">
                <Field label="Subdomain">
                  <Input
                    value={subdomain}
                    onChange={(e) => setSubdomain(e.target.value)}
                    placeholder="grafana"
                    className="h-9 font-mono text-sm"
                  />
                </Field>
                <Field label="Domain">
                  <Select value={chosenDomain} onValueChange={(v) => setDomainId(v ?? "")}>
                    <SelectTrigger className="w-full data-[size=default]:h-9 text-sm bg-muted/20 border-border/60">
                      <SelectValue>{verified.find((d) => d.id === chosenDomain)?.base_domain}</SelectValue>
                    </SelectTrigger>
                    <SelectContent>
                      {verified.map((d) => (
                        <SelectItem key={d.id} value={d.id}>{d.base_domain}</SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </Field>
              </div>
              )
            ) : (
              <Field label="Hostname" hint="No verified domain yet, so the hostname is given in full.">
                <Input
                  value={hostname}
                  onChange={(e) => setHostname(e.target.value)}
                  placeholder="grafana.example.com"
                  className="h-9 font-mono text-sm"
                />
              </Field>
            )
          ) : (
            <div className="flex flex-col gap-4">
              <Field label="Zone" hint="Public opens the port on every interface. Mesh keeps it to the mesh; local to the gateway itself.">
                <SegmentedControl
                  value={zone}
                  onValueChange={(v) => setZone(v as TCPZone)}
                  options={[
                    { value: "public", label: "Public" },
                    { value: "mesh", label: "Mesh" },
                    { value: "local", label: "Local" },
                  ]}
                />
              </Field>
              {zone === "public" && (
                <Field
                  label="Gateway port"
                  hint={endpoint.suggested_port
                    ? "The port the gateway listens on. It need not match the target's."
                    : `The gateway keeps ${endpoint.port} for itself, so this route needs a different one.`}
                >
                  <Input
                    value={gatewayPort}
                    onChange={(e) => setGatewayPort(e.target.value.replace(/[^0-9]/g, ""))}
                    placeholder={endpoint.suggested_port ? String(endpoint.suggested_port) : "e.g. 15432"}
                    inputMode="numeric"
                    className="h-9 w-32 font-mono text-sm"
                  />
                </Field>
              )}
            </div>
          )}
        </div>

        {create.isError && (
          <p role="alert" className="text-sm text-destructive">{(create.error as Error).message}</p>
        )}

        <DialogFooter>
          <Button variant="outline" disabled={create.isPending} onClick={onClose}>Cancel</Button>
          <Button disabled={!canCreate || create.isPending} onClick={() => create.mutate()}>
            {create.isPending && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
            Create route
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function Field({ label, hint, children }: { label: string; hint?: string; children: React.ReactNode }) {
  return (
    <div className="flex flex-col gap-2">
      <label className="text-xs font-medium text-muted-foreground">{label}</label>
      {children}
      {hint && <p className="text-[11px] leading-relaxed text-muted-foreground/70">{hint}</p>}
    </div>
  )
}

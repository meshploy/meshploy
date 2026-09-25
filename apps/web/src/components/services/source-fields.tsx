import { useQuery } from "@tanstack/react-query"
import { gitIntegrations as gitApi, registries as registryApi } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { SegmentedControl } from "@/components/ui/segmented-control"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { cn } from "@/lib/utils"
import { useEffect } from "react"
import { Field, inputCls } from "@/components/services/form-primitives"

export interface SourceState {
  source: "git" | "image"
  image: string
  imageVisibility: "public" | "private"
  pullRegistryIntegrationId: string
  gitVisibility: "public" | "private"
  gitIntegrationId: string
  gitRepo: string
  gitBranch: string
  builder: "railpack" | "dockerfile"
  dockerfilePath: string
  /** Empty: the builder works it out, as it always has. */
  installCommand: string
  buildCommand: string
  registryIntegrationId: string
}

export function SourceFields({
  value: f,
  onChange,
  pickDefaultRegistry = false,
}: {
  value: SourceState
  onChange: (patch: Partial<SourceState>) => void
  /** A new service: choose the built-in registry (or the only one) for it,
   *  the way the builder is chosen for it. Off where a saved choice is shown. */
  pickDefaultRegistry?: boolean
}) {
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)!

  const { data: gitList = [] } = useQuery({
    queryKey: ["git-integrations", orgId],
    queryFn: () => gitApi.list(orgId, token),
    enabled: !!orgId,
  })
  const connectedGit = gitList.filter((g) => g.connected)

  const { data: registryList = [] } = useQuery({
    queryKey: ["registry-integrations", orgId],
    queryFn: () => registryApi.list(orgId, token),
    enabled: !!orgId,
  })

  const fallbackRegistry =
    registryList.find((r) => r.provider === "builtin") ?? (registryList.length === 1 ? registryList[0] : undefined)
  useEffect(() => {
    if (pickDefaultRegistry && !f.registryIntegrationId && fallbackRegistry) {
      onChange({ registryIntegrationId: fallbackRegistry.id })
    }
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [pickDefaultRegistry, f.registryIntegrationId, fallbackRegistry?.id])

  const { data: repoList = [], isFetching: reposFetching } = useQuery({
    queryKey: ["git-repos", orgId, f.gitIntegrationId],
    queryFn: () => gitApi.repos(orgId, f.gitIntegrationId, token),
    enabled: !!f.gitIntegrationId,
    staleTime: 5 * 60 * 1000,
  })

  const { data: branchList = [], isFetching: branchesFetching } = useQuery({
    queryKey: ["git-branches", orgId, f.gitIntegrationId, f.gitRepo],
    queryFn: () => gitApi.branches(orgId, f.gitIntegrationId, f.gitRepo, token),
    enabled: !!f.gitIntegrationId && !!f.gitRepo,
    staleTime: 2 * 60 * 1000,
  })

  return (
    <div className="space-y-4">
      <SegmentedControl
        value={f.source}
        onValueChange={(v) => onChange({ source: v as "git" | "image" })}
        options={[
          { value: "git",   label: "Git repository" },
          { value: "image", label: "Docker image" },
        ]}
        className="text-sm"
      />

      {f.source === "image" ? (
        <div className="space-y-4">
          <Field label="Image" required>
            <input
              value={f.image}
              onChange={(e) => onChange({ image: e.target.value })}
              placeholder="nginx:alpine"
              className={inputCls}
            />
          </Field>
          <SegmentedControl
            value={f.imageVisibility}
            onValueChange={(v) => onChange({ imageVisibility: v as "public" | "private", pullRegistryIntegrationId: "" })}
            options={[
              { value: "public",  label: "Public" },
              { value: "private", label: "Private" },
            ]}
            className="text-sm"
          />
          {f.imageVisibility === "private" && (
            <Field label="Pull registry" required>
              <Select
                value={f.pullRegistryIntegrationId}
                onValueChange={(v) => onChange({ pullRegistryIntegrationId: v ?? "" })}
              >
                <SelectTrigger className="w-full! h-9 text-sm bg-muted/20 border-border/60">
                  <SelectValue
                    placeholder={
                      registryList.length === 0
                        ? "No registries — add one in Integrations"
                        : "Select a registry to pull the image…"
                    }
                  >
                    {registryList.find((r) => r.id === f.pullRegistryIntegrationId)?.name}
                  </SelectValue>
                </SelectTrigger>
                <SelectContent>
                  {registryList.map((r) => (
                    <SelectItem key={r.id} value={r.id}>{r.name}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>
          )}
        </div>
      ) : (
        <div className="space-y-4">
          <SegmentedControl
            value={f.gitVisibility}
            onValueChange={(v) => onChange({ gitVisibility: v as "public" | "private", gitIntegrationId: "", gitRepo: "", gitBranch: "" })}
            options={[
              { value: "public",  label: "Public" },
              { value: "private", label: "Private" },
            ]}
            className="text-sm"
          />

          {f.gitVisibility === "private" && (
            <Field label="Git integration" required>
              <Select
                value={f.gitIntegrationId}
                onValueChange={(v) => onChange({ gitIntegrationId: v ?? "", gitRepo: "", gitBranch: "" })}
              >
                <SelectTrigger className="w-full! h-9 text-sm bg-muted/20 border-border/60">
                  <SelectValue
                    placeholder={
                      connectedGit.length === 0
                        ? "No connected integrations — add one in Integrations"
                        : "Select a git integration…"
                    }
                  >
                    {connectedGit.find((g) => g.id === f.gitIntegrationId)?.name}
                  </SelectValue>
                </SelectTrigger>
                <SelectContent>
                  {connectedGit.map((g) => (
                    <SelectItem key={g.id} value={g.id}>{g.name}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>
          )}

          <div className="grid grid-cols-2 gap-4">
            {f.gitVisibility === "private" ? (
              <Field label={reposFetching ? "Repository (loading…)" : "Repository"} required>
                <Select
                  value={f.gitRepo}
                  onValueChange={(v) => {
                    const repo = repoList.find((r) => r.full_name === v)
                    onChange({ gitRepo: v ?? "", gitBranch: repo?.default_branch ?? f.gitBranch })
                  }}
                  disabled={!f.gitIntegrationId || reposFetching}
                >
                  <SelectTrigger className="w-full! h-9 text-sm bg-muted/20 border-border/60">
                    <SelectValue
                      placeholder={
                        !f.gitIntegrationId
                          ? "Select an integration first"
                          : reposFetching
                          ? "Loading repositories…"
                          : repoList.length === 0
                          ? "No accessible repositories"
                          : "Select a repository…"
                      }
                    />
                  </SelectTrigger>
                  <SelectContent>
                    {f.gitRepo && !repoList.find((r) => r.full_name === f.gitRepo) && (
                      <SelectItem value={f.gitRepo}>{f.gitRepo}</SelectItem>
                    )}
                    {repoList.map((r) => (
                      <SelectItem key={r.full_name} value={r.full_name}>{r.full_name}</SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </Field>
            ) : (
              <Field label="Repository URL" required>
                <input
                  value={f.gitRepo}
                  onChange={(e) => onChange({ gitRepo: e.target.value })}
                  placeholder="https://github.com/owner/repo"
                  className={inputCls}
                />
              </Field>
            )}

            {f.gitVisibility === "private" ? (
              <Field label={branchesFetching ? "Branch (loading…)" : "Branch"} required>
                <Select
                  value={f.gitBranch}
                  onValueChange={(v) => onChange({ gitBranch: v ?? "" })}
                  disabled={!f.gitRepo || branchesFetching}
                >
                  <SelectTrigger className="w-full! h-9 text-sm bg-muted/20 border-border/60">
                    <SelectValue
                      placeholder={
                        !f.gitRepo
                          ? "Select a repo first"
                          : branchesFetching
                          ? "Loading branches…"
                          : "Select a branch…"
                      }
                    />
                  </SelectTrigger>
                  <SelectContent>
                    {f.gitBranch && !branchList.find((b) => b === f.gitBranch) && (
                      <SelectItem value={f.gitBranch}>{f.gitBranch}</SelectItem>
                    )}
                    {branchList.map((b) => (
                      <SelectItem key={b} value={b}>{b}</SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </Field>
            ) : (
              <Field label="Branch" required>
                <input
                  value={f.gitBranch}
                  onChange={(e) => onChange({ gitBranch: e.target.value })}
                  placeholder="main"
                  className={inputCls}
                />
              </Field>
            )}
          </div>

          <div className="grid grid-cols-2 gap-4">
            <Field label="Builder">
              <Select
                value={f.builder}
                onValueChange={(v) => onChange({ builder: (v ?? "railpack") as "railpack" | "dockerfile" })}
              >
                <SelectTrigger className="w-full! h-9 text-sm bg-muted/20 border-border/60">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="railpack">Railpack</SelectItem>
                  <SelectItem value="dockerfile">Dockerfile</SelectItem>
                </SelectContent>
              </Select>
            </Field>
            {f.builder === "dockerfile" && (
              <Field label="Dockerfile path">
                <input
                  value={f.dockerfilePath}
                  onChange={(e) => onChange({ dockerfilePath: e.target.value })}
                  placeholder="Dockerfile"
                  className={inputCls}
                />
              </Field>
            )}
          </div>

          {/* Left empty, Railpack works both out from the code, as it always
              has; a Dockerfile holds its own steps. */}
          {f.builder === "dockerfile" ? (
            <p className="text-xs text-muted-foreground">The install and build steps are the Dockerfile&apos;s own.</p>
          ) : (
            <div className="grid grid-cols-2 gap-4" data-testid="build-commands">
              <Field label="Install command" term="services.install-command">
                <input
                  value={f.installCommand}
                  onChange={(e) => onChange({ installCommand: e.target.value })}
                  placeholder="Worked out by Railpack"
                  className={cn(inputCls, "font-mono")}
                />
              </Field>
              <Field label="Build command" term="services.build-command">
                <input
                  value={f.buildCommand}
                  onChange={(e) => onChange({ buildCommand: e.target.value })}
                  placeholder="Worked out by Railpack"
                  className={cn(inputCls, "font-mono")}
                />
              </Field>
            </div>
          )}

          <Field label="Registry (push destination)" required>
            <Select
              value={f.registryIntegrationId}
              onValueChange={(v) => onChange({ registryIntegrationId: v ?? "" })}
            >
              <SelectTrigger className="w-full! h-9 text-sm bg-muted/20 border-border/60">
                <SelectValue
                  placeholder={
                    registryList.length === 0
                      ? "No registries — add one in Integrations"
                      : "Select a registry to push the built image…"
                  }
                >
                  {registryList.find((r) => r.id === f.registryIntegrationId)?.name}
                </SelectValue>
              </SelectTrigger>
              <SelectContent>
                {registryList.map((r) => (
                  <SelectItem key={r.id} value={r.id}>{r.name}</SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
        </div>
      )}
    </div>
  )
}

import { createFileRoute, useParams } from "@tanstack/react-router"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { ResourcePermissionsSection } from "@/components/permissions/resource-permissions"

export const Route = createFileRoute(
  "/_app/projects/$id/jobs/$jobId/permissions"
)({
  component: JobPermissionsTab,
})

function JobPermissionsTab() {
  const { id: projectId, jobId } = useParams({
    from: "/_app/projects/$id/jobs/$jobId/permissions",
  })
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)!

  return (
    <div className="p-6 max-w-2xl space-y-6">
      <div>
        <h1 className="text-xl font-semibold tracking-tight">Permissions</h1>
        <p className="text-sm text-muted-foreground mt-0.5">
          Override project-level access for specific members on this job
        </p>
      </div>
      <ResourcePermissionsSection
        orgId={orgId}
        projectId={projectId}
        resourceType="job"
        resourceId={jobId}
        token={token}
      />
    </div>
  )
}

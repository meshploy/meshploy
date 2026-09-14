import { ResourceIntro } from "@/components/layout/resource-workbench"
import { createFileRoute, useParams } from "@tanstack/react-router"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { ResourcePermissionsSection } from "@/components/permissions/resource-permissions"

export const Route = createFileRoute(
  "/_app/projects/$id/stacks/$stackId/permissions"
)({
  component: StackPermissionsTab,
})

function StackPermissionsTab() {
  const { id: projectId, stackId } = useParams({
    from: "/_app/projects/$id/stacks/$stackId/permissions",
  })
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)!

  return (
    <div className="console-page space-y-6">
      <ResourceIntro title="Permissions" description="Override project-level access for specific members on this stack" />
      <ResourcePermissionsSection
        orgId={orgId}
        projectId={projectId}
        resourceType="stack"
        resourceId={stackId}
        token={token}
      />
    </div>
  )
}

import { ResourceIntro } from "@/components/layout/resource-workbench"
import { createFileRoute, useParams } from "@tanstack/react-router"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { ResourcePermissionsSection } from "@/components/permissions/resource-permissions"
import { EeServiceAccess } from "@/ee"

export const Route = createFileRoute(
  "/_app/projects/$id/services/$serviceId/permissions"
)({
  component: ServicePermissionsTab,
})

function ServicePermissionsTab() {
  const { id: projectId, serviceId } = useParams({
    from: "/_app/projects/$id/services/$serviceId/permissions",
  })
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)!

  return (
    <div className="console-page space-y-6">
      {EeServiceAccess && (
        <>
          <ResourceIntro title="Access" description="Who outside your team may open this service, and which members may manage it" />
          <EeServiceAccess orgId={orgId} projectId={projectId} serviceId={serviceId} />
        </>
      )}
      <ResourceIntro title={EeServiceAccess ? "Your team" : "Access"} description="Override project-level access for specific members on this service" />
      <ResourcePermissionsSection
        orgId={orgId}
        projectId={projectId}
        resourceType="service"
        resourceId={serviceId}
        token={token}
      />
    </div>
  )
}

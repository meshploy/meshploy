import { ResourceIntro, ResourcePanel } from "@/components/layout/resource-workbench"
import { createFileRoute, useParams } from "@tanstack/react-router"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { ResourcePermissionsSection } from "@/components/permissions/resource-permissions"
import { useQuery } from "@tanstack/react-query"
import { services } from "@/lib/api"
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
  const { data: svc } = useQuery({
    queryKey: ["service", orgId, projectId, serviceId],
    queryFn: () => services.get(orgId, projectId, serviceId, token),
    enabled: !!orgId,
  })
  // Who outside the team may open it is a sign-in in front of a web address,
  // so it is offered only for a service that serves one. A database or any
  // other TCP service is reached over the mesh, which the section below covers.
  const Outside = EeServiceAccess && svc?.ports?.some((p) => p.is_http) ? EeServiceAccess : null

  const manage = (
    <ResourcePermissionsSection
      orgId={orgId}
      projectId={projectId}
      resourceType="service"
      resourceId={serviceId}
      token={token}
    />
  )

  return (
    <div className="console-page space-y-6">
      {Outside && (
        <>
          <ResourceIntro title="Access" description="Who can open this service, and who can manage it in the console" />
          <Outside orgId={orgId} projectId={projectId} serviceId={serviceId} />
        </>
      )}
      {Outside ? (
        <ResourcePanel title="Who can manage it" description="What members may do with this service in the console.">
          {manage}
        </ResourcePanel>
      ) : (
        <>
          <ResourceIntro title="Access" description="What members may do with this service in the console" />
          {manage}
        </>
      )}
    </div>
  )
}

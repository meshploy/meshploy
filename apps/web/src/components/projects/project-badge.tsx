import { EeProjectBadge } from "@/ee"

/** What an edition draws beside a project's name; nothing in Community. */
export function ProjectBadge({ projectId }: { projectId: string }) {
  return EeProjectBadge ? <EeProjectBadge projectId={projectId} /> : null
}

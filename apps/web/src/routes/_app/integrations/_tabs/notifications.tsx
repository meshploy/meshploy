import { createFileRoute } from "@tanstack/react-router"
import { NotificationsTab } from "@/components/integrations/sections"

export const Route = createFileRoute("/_app/integrations/_tabs/notifications")({
  component: NotificationsTab,
})

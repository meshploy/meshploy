import { createFileRoute } from "@tanstack/react-router"
import { EmailProviderTab } from "@/components/integrations/sections"

export const Route = createFileRoute("/_app/integrations/_tabs/email")({
  component: EmailProviderTab,
})

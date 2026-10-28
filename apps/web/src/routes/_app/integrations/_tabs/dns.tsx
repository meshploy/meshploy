import { createFileRoute } from "@tanstack/react-router"
import { DNSProvidersTab } from "@/components/integrations/sections"

export const Route = createFileRoute("/_app/integrations/_tabs/dns")({
  component: DNSProvidersTab,
})

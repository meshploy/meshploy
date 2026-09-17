import { createFileRoute } from "@tanstack/react-router"
import { RegistriesTab } from "@/components/integrations/sections"

export const Route = createFileRoute("/_app/integrations/_tabs/registries")({
  component: RegistriesTab,
})

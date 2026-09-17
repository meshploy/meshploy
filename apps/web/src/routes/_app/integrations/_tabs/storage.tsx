import { createFileRoute } from "@tanstack/react-router"
import { StorageTab } from "@/components/integrations/sections"

export const Route = createFileRoute("/_app/integrations/_tabs/storage")({
  component: StorageTab,
})

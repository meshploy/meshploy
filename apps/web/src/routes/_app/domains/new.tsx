import { createFileRoute } from "@tanstack/react-router"
import { DomainSetupWizard } from "@/components/domains/domain-setup-wizard"

export const Route = createFileRoute("/_app/domains/new")({
  component: NewDomainPage,
})

function NewDomainPage() {
  return <DomainSetupWizard />
}

import { createFileRoute } from "@tanstack/react-router"
import { Section } from "@/components/services/form-primitives"
import { ConnectionDetails, MySessions } from "@/components/auth/connected-sessions"
import { KeysSection } from "@/components/agents/keys-section"
import { HelpButton } from "@/help/help-button"
import { useIsAdmin } from "@/store/org-store"

// Everything that connects to Meshploy from outside the console: how an AI
// assistant or editor connects, what is signed in as you now, and the keys
// for CI and scripts. Members see the first two; keys are for admins.

export const Route = createFileRoute("/_app/connectors/")({
  component: ConnectorsPage,
})

function ConnectorsPage() {
  const isAdmin = useIsAdmin()
  return (
    <div className="console-page space-y-6">
      <div>
        <h1 className="flex items-center gap-2 text-xl font-semibold tracking-tight"><span>Connectors</span><HelpButton topic="agents" label="How connecting works" /></h1>
        <p className="mt-0.5 text-sm text-muted-foreground">Connect Claude, Cursor, VS Code or a terminal to Meshploy, and see what is signed in as you.</p>
      </div>
      <Section title="Connection details" subtitle="Meshploy's MCP address, and how each client adds it. Each one signs in here, so nothing secret goes into a config file.">
        <ConnectionDetails />
      </Section>
      <Section title="Connected sessions" subtitle="Everything signed in as you: AI assistants in this organisation, and CLIs. One unused for 90 days ends by itself.">
        <MySessions />
      </Section>
      {isAdmin && <KeysSection />}
    </div>
  )
}

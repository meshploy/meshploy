import { createFileRoute } from "@tanstack/react-router"
import { Bell, Box, HardDrive, Plus } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { mockRegistries, mockStorage, mockNotifications } from "@/lib/mock-data"
import type { RegistryIntegration, StorageIntegration, NotificationChannel } from "@/types"

const PROVIDER_LABELS: Record<string, string> = {
  ghcr: "GitHub Container Registry", dockerhub: "Docker Hub", ecr: "Amazon ECR", generic: "Private Registry",
  s3: "Amazon S3", r2: "Cloudflare R2", minio: "MinIO",
  slack: "Slack", webhook: "Webhook", email: "Email",
}

export const Route = createFileRoute("/_app/integrations/")({
  component: IntegrationsPage,
})

function IntegrationsPage() {
  return (
    <div className="p-6 space-y-8">
      <div>
        <h1 className="text-xl font-semibold tracking-tight">Integrations</h1>
        <p className="text-sm text-muted-foreground mt-0.5">Connect external services for image pulls, backups, and alerts</p>
      </div>

      <Section icon={<Box className="h-4 w-4" />} title="Container Registries" description="Pull images from private and public registries" action="Add registry">
        {mockRegistries.map((reg) => (
          <IntegrationCard key={reg.id} name={reg.name} providerKey={reg.provider} meta={reg.endpoint ?? `docker.io/${reg.username}`} />
        ))}
      </Section>

      <Section icon={<HardDrive className="h-4 w-4" />} title="Object Storage" description="Store database backups and build artifacts" action="Add storage">
        {mockStorage.map((sto) => (
          <IntegrationCard key={sto.id} name={sto.name} providerKey={sto.provider} meta={`${sto.bucket}${sto.region ? ` · ${sto.region}` : ""}`} />
        ))}
      </Section>

      <Section icon={<Bell className="h-4 w-4" />} title="Notification Channels" description="Get alerted on deployments, node status changes, and failures" action="Add channel">
        {mockNotifications.map((ntf) => <NotificationCard key={ntf.id} channel={ntf} />)}
      </Section>
    </div>
  )
}

function Section({ icon, title, description, action, children }: { icon: React.ReactNode; title: string; description: string; action: string; children: React.ReactNode }) {
  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <div className="text-muted-foreground">{icon}</div>
          <div>
            <h2 className="text-sm font-medium text-foreground">{title}</h2>
            <p className="text-xs text-muted-foreground">{description}</p>
          </div>
        </div>
        <button className="flex items-center gap-1.5 text-xs text-muted-foreground border border-border/60 px-2.5 py-1.5 rounded-md hover:text-foreground hover:border-border transition-colors">
          <Plus className="h-3 w-3" />{action}
        </button>
      </div>
      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">{children}</div>
    </div>
  )
}

function IntegrationCard({ name, providerKey, meta }: { name: string; providerKey: string; meta: string }) {
  return (
    <div className="flex items-start gap-3 rounded-lg border border-border/60 bg-card p-4">
      <div className="flex items-center justify-center w-8 h-8 rounded-md bg-muted shrink-0 text-xs font-bold text-muted-foreground uppercase">
        {providerKey.slice(0, 2)}
      </div>
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2">
          <p className="text-sm font-medium text-foreground truncate">{name}</p>
          <Badge variant="secondary" className="text-[10px] px-1.5 py-0 h-4 shrink-0 bg-emerald-500/10 text-emerald-400 border-0">connected</Badge>
        </div>
        <p className="text-xs text-muted-foreground mt-0.5">{PROVIDER_LABELS[providerKey] ?? providerKey}</p>
        <p className="text-[11px] font-mono text-muted-foreground/60 mt-1 truncate">{meta}</p>
      </div>
    </div>
  )
}

function NotificationCard({ channel }: { channel: NotificationChannel }) {
  return (
    <div className="flex flex-col gap-3 rounded-lg border border-border/60 bg-card p-4">
      <div className="flex items-center gap-3">
        <div className="flex items-center justify-center w-8 h-8 rounded-md bg-muted shrink-0 text-xs font-bold text-muted-foreground uppercase">
          {channel.type.slice(0, 2)}
        </div>
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2">
            <p className="text-sm font-medium text-foreground truncate">{channel.name}</p>
            <Badge variant="secondary" className="text-[10px] px-1.5 py-0 h-4 shrink-0 bg-emerald-500/10 text-emerald-400 border-0">active</Badge>
          </div>
          <p className="text-xs text-muted-foreground">{PROVIDER_LABELS[channel.type]}</p>
        </div>
      </div>
      <div className="flex flex-wrap gap-1">
        {channel.events.map((ev) => (
          <code key={ev} className="text-[10px] font-mono bg-muted/60 px-1.5 py-0.5 rounded text-muted-foreground">{ev}</code>
        ))}
      </div>
    </div>
  )
}

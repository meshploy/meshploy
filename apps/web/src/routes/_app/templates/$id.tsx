import { createFileRoute, Link, useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  ChevronLeft,
  ChevronDown,
  ExternalLink,
  KeyRound,
  Loader2,
  Pencil,
  ServerCrash,
} from "lucide-react";
import { templates as templatesApi } from "@/lib/api";
import { useAuthStore } from "@/store/auth-store";
import { Button } from "@/components/ui/button";
import { TemplateLogo } from "@/components/templates/template-logo";
import { UseTemplateDialog } from "@/components/templates/use-template-dialog";
import { cn } from "@/lib/utils";

export const Route = createFileRoute("/_app/templates/$id")({
  component: TemplateDetailPage,
});

function TemplateDetailPage() {
  const { id } = Route.useParams();
  const token = useAuthStore((s) => s.token)!;
  const navigate = useNavigate();

  const [showCompose, setShowCompose] = useState(false);

  const { data, isLoading, isError, error } = useQuery({
    queryKey: ["template", id],
    queryFn: () => templatesApi.get(id, token),
  });

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-64 gap-2 text-muted-foreground">
        <Loader2 className="h-4 w-4 animate-spin" />
        <span className="text-sm">Loading template…</span>
      </div>
    );
  }

  if (isError || !data) {
    return (
      <div className="flex flex-col items-center justify-center h-64 gap-3 text-muted-foreground">
        <ServerCrash className="h-8 w-8 text-destructive/60" />
        <p className="text-sm">Template not found</p>
        {error && (
          <p className="text-xs text-muted-foreground/60">
            {(error as Error).message}
          </p>
        )}
        <Link to="/templates" className="text-xs text-primary hover:underline">
          Back to templates
        </Link>
      </div>
    );
  }

  const { manifest, compose } = data;
  const prompted = manifest.variables?.filter((v) => v.prompt) ?? [];
  const generated = manifest.variables?.filter((v) => v.generate) ?? [];

  return (
    <div className="min-h-screen bg-background flex flex-col">
      {/* Top bar */}
      <div className="sticky top-0 z-10 border-b border-border/40 bg-background/90 backdrop-blur-sm">
        <div className="h-14 flex items-center gap-3 px-6">
          <Button
            variant="ghost"
            onClick={() => navigate({ to: "/templates" })}
            className="flex items-center gap-1.5 text-sm text-muted-foreground hover:text-foreground transition-colors"
          >
            <ChevronLeft className="h-4 w-4" />
            Templates
          </Button>
          <span className="text-muted-foreground/40">/</span>
          <span className="text-sm font-medium">{manifest.name}</span>
        </div>
      </div>

      <div className="flex-1 flex items-start justify-center py-12 px-6">
        <div className="w-full max-w-2xl space-y-6">
          {/* Header */}
          <div className="flex items-start gap-4">
            <TemplateLogo
              id={id}
              name={manifest.name}
              className="w-12 h-12 text-lg"
            />
            <div className="min-w-0 flex-1">
              <div className="flex items-center gap-2 flex-wrap">
                <h1 className="text-lg font-semibold tracking-tight">
                  {manifest.name}
                </h1>
                <span className="text-[11px] px-1.5 py-0.5 rounded bg-muted/50 text-muted-foreground capitalize">
                  {manifest.category}
                </span>
                <span className="text-[10px] font-mono text-muted-foreground/50">
                  v{manifest.version}
                </span>
              </div>
              <p className="text-sm text-muted-foreground mt-1">
                {manifest.description}
              </p>
              <div className="flex items-center gap-4 mt-2">
                {manifest.links?.website && (
                  <a
                    href={manifest.links.website}
                    target="_blank"
                    rel="noopener"
                    className="inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
                  >
                    Website <ExternalLink className="h-3 w-3" />
                  </a>
                )}
                {manifest.links?.source && (
                  <a
                    href={manifest.links.source}
                    target="_blank"
                    rel="noopener"
                    className="inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
                  >
                    Source <ExternalLink className="h-3 w-3" />
                  </a>
                )}
              </div>
            </div>
          </div>

          {/* Deploy card */}
          <div className="flex items-center justify-between rounded-xl border border-border/60 bg-card p-4">
            <div>
              <p className="text-sm font-medium">Deploy this template</p>
              <p className="text-[11px] text-muted-foreground/70 mt-0.5">
                Pick a project, then review before it deploys.
              </p>
            </div>
            <UseTemplateDialog
              templateId={id}
              templateName={manifest.name}
              trigger={<Button>Use template</Button>}
            />
          </div>

          {/* Variables */}
          {(prompted.length > 0 || generated.length > 0) && (
            <div className="rounded-xl border border-border/60 bg-card p-4 space-y-3">
              <p className="text-sm font-medium">Configuration</p>
              {prompted.map((v) => (
                <div key={v.key} className="flex items-center gap-2 text-xs">
                  <Pencil className="h-3.5 w-3.5 text-muted-foreground shrink-0" />
                  <span className="font-mono text-foreground">{v.key}</span>
                  <span className="text-muted-foreground">
                    — {v.prompt || "you'll be asked"}
                  </span>
                  {v.required && (
                    <span className="text-[10px] text-amber-400">required</span>
                  )}
                </div>
              ))}
              {generated.map((v) => (
                <div key={v.key} className="flex items-center gap-2 text-xs">
                  <KeyRound className="h-3.5 w-3.5 text-muted-foreground shrink-0" />
                  <span className="font-mono text-foreground">{v.key}</span>
                  <span className="text-muted-foreground">
                    — auto-generated ({v.generate})
                  </span>
                </div>
              ))}
            </div>
          )}

          {/* Compose preview */}
          <div className="rounded-xl border border-border/60 bg-card overflow-hidden">
            <button
              onClick={() => setShowCompose((s) => !s)}
              className="w-full flex items-center justify-between px-4 py-3 text-sm text-muted-foreground hover:text-foreground transition-colors"
            >
              <span className="font-medium">Compose spec</span>
              <ChevronDown
                className={cn(
                  "h-4 w-4 transition-transform",
                  showCompose && "rotate-180",
                )}
              />
            </button>
            {showCompose && (
              <pre className="px-4 pb-4 pt-0 text-xs font-mono text-muted-foreground overflow-x-auto max-h-96 overflow-y-auto border-t border-border/40 pt-3">
                {compose}
              </pre>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}

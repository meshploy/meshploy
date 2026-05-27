import { createFileRoute, useParams } from "@tanstack/react-router"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { useState, useEffect } from "react"
import { Loader2, Save } from "lucide-react"
import { Button } from "@/components/ui/button"
import { stacks as stacksApi } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { Section } from "@/components/services/form-primitives"
import CodeMirror from "@uiw/react-codemirror"
import { envLanguage, envTheme } from "@/lib/env-lang"

export const Route = createFileRoute("/_app/projects/$id/stacks/$stackId/variables")({
  component: StackVariablesTab,
})

function varsToText(variables: Record<string, string>): string {
  return Object.entries(variables).map(([k, v]) => `${k}=${v}`).join("\n")
}

function textToVars(text: string): Record<string, string> {
  const vars: Record<string, string> = {}
  for (const line of text.split("\n")) {
    const trimmed = line.trim()
    if (!trimmed || trimmed.startsWith("#")) continue
    const eq = trimmed.indexOf("=")
    if (eq === -1) continue
    const key = trimmed.slice(0, eq).trim()
    if (key) vars[key] = trimmed.slice(eq + 1)
  }
  return vars
}

function StackVariablesTab() {
  const { id: projectId, stackId } = useParams({ from: "/_app/projects/$id/stacks/$stackId/variables" })
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)
  const qc = useQueryClient()
  const queryKey = ["stack", orgId, projectId, stackId]

  const { data: stack, isLoading } = useQuery({
    queryKey,
    queryFn: () => stacksApi.get(orgId!, projectId, stackId, token),
    enabled: !!orgId,
  })

  const [text, setText] = useState("")
  const [dirty, setDirty] = useState(false)

  useEffect(() => {
    if (stack && !dirty) {
      setText(varsToText(stack.variables ?? {}))
    }
  }, [stack, dirty])

  const saveMutation = useMutation({
    mutationFn: () =>
      stacksApi.update(orgId!, projectId, stackId, { variables: textToVars(text) }, token),
    onSuccess: (updated) => {
      qc.setQueryData(queryKey, updated)
      setDirty(false)
    },
  })

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-40">
        <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
      </div>
    )
  }

  return (
    <div className="p-6 max-w-2xl space-y-6">
      <Section
        title="Stack Variables"
        subtitle="One KEY=VALUE pair per line. Substituted into ${KEY} placeholders in your spec."
      >
        <div className="rounded-md overflow-hidden border border-border/60">
          <CodeMirror
            value={text}
            height="260px"
            theme="dark"
            extensions={[envLanguage, envTheme]}
            onChange={(val) => { setText(val); setDirty(val !== varsToText(stack?.variables ?? {})) }}
            placeholder={"JWT_SECRET=your-secret\nPOSTGRES_PASSWORD=admin123\nAPI_KEY=..."}
            style={{ fontSize: 12 }}
            basicSetup={{ lineNumbers: true, foldGutter: false, autocompletion: false }}
          />
        </div>

        {saveMutation.isError && (
          <p className="text-xs text-destructive">
            {(saveMutation.error as Error)?.message ?? "Failed to save"}
          </p>
        )}

        <div className="flex items-center gap-3">
          <Button
            size="sm"
            className="gap-1.5"
            onClick={() => saveMutation.mutate()}
            disabled={saveMutation.isPending || !dirty}
          >
            {saveMutation.isPending
              ? <Loader2 className="h-3.5 w-3.5 animate-spin" />
              : <Save className="h-3.5 w-3.5" />
            }
            Save
          </Button>
          {dirty && <span className="text-[11px] text-amber-400/80 font-mono">unsaved changes</span>}
        </div>
      </Section>
    </div>
  )
}

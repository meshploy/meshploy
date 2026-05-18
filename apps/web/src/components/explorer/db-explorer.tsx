import { useState, useRef } from "react"
import { useQuery, useMutation } from "@tanstack/react-query"
import { ChevronDown, ChevronRight, Loader2, Play, Terminal } from "lucide-react"
import { services as servicesApi, type ApiSchemaTable, type ApiQueryResult } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"
import { Button } from "@/components/ui/button"
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from "@/components/ui/table"
import { Textarea } from "@/components/ui/textarea"

// ─── Schema tree ──────────────────────────────────────────────────────────────

export function SchemaTree({ tables }: { tables: ApiSchemaTable[] }) {
  const [open, setOpen] = useState<Record<string, boolean>>({})
  if (tables.length === 0) return (
    <p className="text-xs text-muted-foreground/50 px-3 py-4">No tables found</p>
  )
  return (
    <div className="text-xs">
      {tables.map((t) => (
        <div key={t.name}>
          <Button
            variant="ghost"
            onClick={() => setOpen((s) => ({ ...s, [t.name]: !s[t.name] }))}
            className="w-full flex items-center gap-1.5 px-3 py-1.5 hover:bg-muted/30 transition-colors text-left"
          >
            {open[t.name]
              ? <ChevronDown className="h-3 w-3 shrink-0 text-muted-foreground/60" />
              : <ChevronRight className="h-3 w-3 shrink-0 text-muted-foreground/60" />
            }
            <span className="font-mono text-foreground/80 truncate">{t.name}</span>
            <span className="ml-auto text-muted-foreground/40 shrink-0">{t.columns.length}</span>
          </Button>
          {open[t.name] && (
            <div className="pl-7 pb-1">
              {t.columns.map((c) => (
                <div key={c.name} className="flex items-center gap-2 py-0.5 px-2">
                  <span className="font-mono text-foreground/60 truncate">{c.name}</span>
                  <span className="text-muted-foreground/40 shrink-0 text-[10px]">{c.data_type}</span>
                </div>
              ))}
            </div>
          )}
        </div>
      ))}
    </div>
  )
}

// ─── Results table ────────────────────────────────────────────────────────────

export function ResultsTable({ result }: { result: ApiQueryResult }) {
  if (!result.columns?.length) return (
    <p className="text-xs text-muted-foreground/50 p-4">Query executed — no rows returned.</p>
  )
  return (
    <div className="overflow-auto">
      <Table className="text-xs">
        <TableHeader className="bg-muted/20">
          <TableRow className="border-b border-border/40 hover:bg-transparent">
            {result.columns.map((col) => (
              <TableHead key={col} className="px-3 py-2 font-medium text-muted-foreground/70 whitespace-nowrap">{col}</TableHead>
            ))}
          </TableRow>
        </TableHeader>
        <TableBody>
          {(result.rows ?? []).map((row, i) => (
            <TableRow key={i} className="border-b border-border/20 hover:bg-muted/10">
              {row.map((cell, j) => (
                <TableCell key={j} className="px-3 py-1.5 font-mono text-foreground/80 whitespace-nowrap max-w-[300px] truncate">{String(cell)}</TableCell>
              ))}
            </TableRow>
          ))}
        </TableBody>
      </Table>
      {result.count >= 200 && (
        <p className="text-[10px] text-muted-foreground/40 px-3 py-2">Showing first 200 rows.</p>
      )}
    </div>
  )
}

// ─── DB Explorer ──────────────────────────────────────────────────────────────

export function DBExplorer({ projectId, serviceId }: { projectId: string; serviceId: string }) {
  const token = useAuthStore((s) => s.token)!
  const orgId = useOrgStore((s) => s.currentOrg?.id)!
  const [query, setQuery] = useState("SELECT * FROM ")
  const [readOnly, setReadOnly] = useState(true)
  const [result, setResult] = useState<ApiQueryResult | null>(null)
  const [queryError, setQueryError] = useState<string | null>(null)
  const textareaRef = useRef<HTMLTextAreaElement>(null)

  const { data: schema = [], isLoading: schemaLoading } = useQuery<ApiSchemaTable[]>({
    queryKey: ["db-schema", orgId, projectId, serviceId],
    queryFn: () => servicesApi.dbSchema(orgId, projectId, serviceId, token),
    staleTime: 60_000,
  })

  const runMutation = useMutation({
    mutationFn: () => servicesApi.dbQuery(orgId, projectId, serviceId, query.trim(), readOnly, token),
    onSuccess: (data) => { setResult(data); setQueryError(null) },
    onError: (e) => { setQueryError((e as Error).message); setResult(null) },
  })

  const handleKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if ((e.ctrlKey || e.metaKey) && e.key === "Enter") {
      e.preventDefault()
      runMutation.mutate()
    }
  }

  return (
    <div className="flex h-full">
      {/* Schema sidebar */}
      <div className="w-52 shrink-0 border-r border-border/40 overflow-y-auto">
        <div className="px-3 py-2 border-b border-border/40 flex items-center gap-1.5">
          <span className="text-[10px] font-medium text-muted-foreground uppercase tracking-wider">Schema</span>
          {schemaLoading && <Loader2 className="h-3 w-3 animate-spin text-muted-foreground/40" />}
        </div>
        <SchemaTree tables={schema} />
      </div>

      {/* Editor + results */}
      <div className="flex-1 flex flex-col min-w-0">
        {/* Query editor */}
        <div className="border-b border-border/40">
          <Textarea
            ref={textareaRef}
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            onKeyDown={handleKeyDown}
            spellCheck={false}
            className="rounded-none border-0 bg-transparent font-mono text-xs text-foreground resize-none outline-none focus-visible:ring-0 focus-visible:border-0 p-3 min-h-[120px]"
            placeholder="SELECT * FROM ..."
          />
          <div className="flex items-center gap-3 px-3 py-2 border-t border-border/30 bg-muted/10">
            <Button
              size="sm"
              className="gap-1.5 h-7"
              onClick={() => runMutation.mutate()}
              disabled={runMutation.isPending || !query.trim()}
            >
              {runMutation.isPending
                ? <Loader2 className="h-3.5 w-3.5 animate-spin" />
                : <Play className="h-3.5 w-3.5" />
              }
              Run
            </Button>
            <span className="text-[10px] text-muted-foreground/40">Ctrl+Enter</span>
            <label className="ml-auto flex items-center gap-1.5 text-xs text-muted-foreground cursor-pointer select-none">
              <input
                type="checkbox"
                checked={readOnly}
                onChange={(e) => setReadOnly(e.target.checked)}
                className="accent-primary"
              />
              Read-only
            </label>
            {!readOnly && (
              <span className="text-[10px] text-amber-400/70">writes enabled</span>
            )}
          </div>
        </div>

        {/* Results */}
        <div className="flex-1 overflow-auto">
          {queryError && (
            <div className="m-3 rounded-md bg-destructive/10 border border-destructive/20 px-3 py-2">
              <p className="text-xs font-mono text-destructive">{queryError}</p>
            </div>
          )}
          {result && <ResultsTable result={result} />}
          {!result && !queryError && (
            <div className="flex items-center justify-center h-full text-muted-foreground/30 gap-2">
              <Terminal className="h-5 w-5" />
              <span className="text-sm">Run a query to see results</span>
            </div>
          )}
        </div>
      </div>
    </div>
  )
}

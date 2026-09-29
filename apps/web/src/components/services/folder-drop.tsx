import { useRef, useState } from "react"
import { FolderUp, FolderOpen, Loader2, X } from "lucide-react"
import { Button } from "@/components/ui/button"
import { cn, formatBytes, formatRelativeTime } from "@/lib/utils"
import { filesFromDrop, filesFromInput, packFolder, type PackedFolder } from "@/lib/pack-folder"

/** What the service was last built from, shown until another folder is picked. */
export interface CurrentUpload {
  name: string
  files?: number
  size?: number
  uploadedAt?: string | null
}

/**
 * A folder from this computer, packed here and sent when the form is saved.
 * Dropped or chosen; what is left out (.git, node_modules, .env files, what
 * the folder's ignore files name) is said, so nothing leaves unexpectedly.
 */
export function FolderDrop({
  value,
  onChange,
  current,
}: {
  value: PackedFolder | null
  onChange: (folder: PackedFolder | null) => void
  current?: CurrentUpload
}) {
  const input = useRef<HTMLInputElement>(null)
  const [over, setOver] = useState(false)
  const [packing, setPacking] = useState<number | null>(null)
  const [error, setError] = useState<string | null>(null)

  async function pack(read: () => Promise<{ name: string; files: { path: string; file: File }[]; skipped?: string[] } | null>) {
    setError(null)
    try {
      const got = await read()
      if (!got) {
        setError("Drop a folder, not a file.")
        return
      }
      setPacking(got.files.length)
      onChange(await packFolder(got.name, got.files, got.skipped))
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setPacking(null)
    }
  }

  const shown = value ?? null
  return (
    <div className="space-y-2" data-testid="folder-drop">
      {current && !value && (
        <p className="text-xs text-muted-foreground" data-testid="current-upload">
          Built from <span className="text-foreground/80 font-medium">{current.name}</span>
          {current.files ? `, ${current.files} files` : ""}
          {current.size ? `, ${formatBytes(current.size)}` : ""}
          {current.uploadedAt ? `, uploaded ${formatRelativeTime(new Date(current.uploadedAt))}` : ""}.
        </p>
      )}
      {shown ? (
        <div className="flex items-start justify-between gap-3 rounded-md border border-border/60 bg-muted/20 px-3 py-2.5">
          <div className="flex items-start gap-2.5 min-w-0">
            <FolderOpen className="h-4 w-4 text-primary/80 shrink-0 mt-0.5" />
            <div className="min-w-0">
              <p className="text-sm font-medium truncate" data-testid="folder-name">{shown.name}</p>
              <p className="text-xs text-muted-foreground">
                {shown.files} {shown.files === 1 ? "file" : "files"} · {formatBytes(shown.archive.size)} packed
                {current ? " · sent when you save" : ""}
              </p>
              {shown.skipped.length > 0 && (
                <p className="text-xs text-muted-foreground mt-1" data-testid="folder-skipped">
                  Left out: {shown.skipped.slice(0, 5).join(", ")}
                  {shown.skipped.length > 5 ? ` and ${shown.skipped.length - 5} more` : ""}
                </p>
              )}
            </div>
          </div>
          <Button variant="ghost" size="icon" className="h-7 w-7 shrink-0 text-muted-foreground" aria-label="Choose another folder"
            onClick={() => onChange(null)}>
            <X className="h-3.5 w-3.5" />
          </Button>
        </div>
      ) : (
        <div
          onDragOver={(e) => { e.preventDefault(); setOver(true) }}
          onDragLeave={() => setOver(false)}
          onDrop={(e) => {
            e.preventDefault()
            setOver(false)
            const items = e.dataTransfer.items
            void pack(() => filesFromDrop(items))
          }}
          className={cn(
            "flex flex-col items-center justify-center gap-2 rounded-md border border-dashed px-4 py-7 text-center transition-colors",
            over ? "border-primary/70 bg-primary/5" : "border-border/60 bg-muted/10",
          )}
        >
          {packing !== null ? (
            <>
              <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
              <p className="text-sm text-muted-foreground">Packing {packing.toLocaleString()} files…</p>
            </>
          ) : (
            <>
              <FolderUp className="h-5 w-5 text-muted-foreground" />
              <p className="text-sm">
                {current ? "Drop a new version of the folder here" : "Drop the app's folder here"}
              </p>
              <Button variant="outline" size="sm" className="h-7 text-xs" onClick={() => input.current?.click()}>
                Choose a folder
              </Button>
              <p className="text-[11px] text-muted-foreground max-w-sm">
                Left out: .git, node_modules, .env files, and what the folder&apos;s .gitignore and .meshployignore name. Up to 100 MB packed.
              </p>
            </>
          )}
          <input
            ref={input}
            type="file"
            className="hidden"
            data-testid="folder-input"
            // A folder, not files: every browser Meshploy supports reads this.
            {...({ webkitdirectory: "", directory: "" } as Record<string, string>)}
            onChange={(e) => {
              const list = e.target.files
              if (list && list.length > 0) void pack(async () => filesFromInput(list))
              e.target.value = ""
            }}
          />
        </div>
      )}
      {error && <p className="text-xs text-destructive" role="alert">{error}</p>}
    </div>
  )
}

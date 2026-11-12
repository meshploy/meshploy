import { dbVersionNote } from "@/lib/db-versions"

/** A database version in a picker: its tag, and what to know about it. */
export function VersionLabel({ engine, tag }: { engine: string; tag: string }) {
  const note = dbVersionNote(engine, tag)
  return (
    <span className="flex items-baseline gap-2">
      <span>{tag}</span>
      {note && <span className="text-xs text-muted-foreground">{note}</span>}
    </span>
  )
}

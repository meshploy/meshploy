import { Input } from "@/components/ui/input"
import { useState } from "react"
import { Search, X } from "lucide-react"

export function useResourceSearch() {
  const [search, setSearch] = useState("")
  const matches = (resource: {
    name?: string
    hostname?: string
    image?: string
    description?: string
  }) =>
    [resource.name, resource.hostname, resource.image, resource.description]
      .filter(Boolean)
      .join(" ")
      .toLowerCase()
      .includes(search.toLowerCase().trim())
  return { search, setSearch, matches }
}
// A list this short is quicker to scan than to search, so the box stays
// hidden until it grows, unless a search is already typed.
const SEARCH_FROM = 9

export function ResourceSearch({
  value,
  onChange,
  label,
  empty,
  count,
}: {
  value: string
  onChange: (value: string) => void
  label: string
  empty?: boolean
  /** How many items the whole list has, before searching. */
  count: number
}) {
  if (count < SEARCH_FROM && !value) return null
  return (
    <div className="space-y-4">
      <div className="relative max-w-md">
        <Search className="absolute left-3 top-1/2 -translate-y-1/2 size-4 text-muted-foreground" />
        <Input
          aria-label={`Search ${label}`}
          value={value}
          onChange={(e) => onChange(e.target.value)}
          placeholder={`Search ${label}…`}
          className="h-10 w-full pl-10 pr-10"
        />
        {value && (
          <button
            aria-label="Clear search"
            className="absolute right-2 top-1/2 -translate-y-1/2 rounded-md p-1 text-muted-foreground hover:text-foreground"
            onClick={() => onChange("")}
          >
            <X className="size-4" />
          </button>
        )}
      </div>
      {empty && value.trim() && (
        <div
          role="status"
          className="rounded-xl border border-dashed border-border p-10 text-center"
        >
          <p className="text-sm font-medium">No matching {label}</p>
          <p className="mt-2 text-xs text-muted-foreground">
            Try another name or clear your search.
          </p>
        </div>
      )}
    </div>
  )
}

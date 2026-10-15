import { useEffect } from "react"
import { useQuery } from "@tanstack/react-query"
import { brand as brandApi, brandAssetURL, DEFAULT_BRAND, type ApiBrand } from "@/lib/api"
import { useAuthStore } from "@/store/auth-store"
import { useOrgStore } from "@/store/org-store"

// The brand last shown, kept in this browser so a branded console does not
// show Meshploy's for a moment on every load.
const LAST_BRAND = "meshploy-brand"

function lastBrand(): ApiBrand | undefined {
  try {
    const raw = localStorage.getItem(LAST_BRAND)
    return raw ? (JSON.parse(raw) as ApiBrand) : undefined
  } catch {
    return undefined
  }
}

function keepBrand(b: ApiBrand) {
  try {
    localStorage.setItem(LAST_BRAND, JSON.stringify(b))
  } catch {
    // Nothing kept: the next load shows the default until the brand arrives.
  }
}

/**
 * The brand people see: the organisation being viewed once signed in, and
 * before that the one whose domain this console is served on.
 */
export function useBrand(): ApiBrand {
  const token = useAuthStore((s) => s.token)
  const orgId = useOrgStore((s) => s.currentOrg?.id)
  const signedIn = !!token && !!orgId
  const { data } = useQuery({
    queryKey: signedIn ? ["brand", orgId] : ["brand", "host"],
    queryFn: async () => {
      const b = signedIn ? await brandApi.forOrg(orgId!, token!) : await brandApi.forHost()
      keepBrand(b)
      return b
    },
    staleTime: 5 * 60_000,
    placeholderData: lastBrand,
  })
  return data ?? DEFAULT_BRAND
}

/** Puts the brand on the browser tab: its name as the title, its mark as the icon. */
export function useBrandDocument(brand: ApiBrand) {
  useEffect(() => {
    document.title = brand.name
    const mark = brandAssetURL(brand.mark_url)
    for (const link of document.querySelectorAll<HTMLLinkElement>('link[rel="icon"], link[rel="apple-touch-icon"]')) {
      // The page's own icons are remembered once, to return to when a brand
      // has no mark.
      if (link.dataset.default === undefined) {
        link.dataset.default = link.getAttribute("href") ?? ""
        link.dataset.defaultType = link.getAttribute("type") ?? ""
      }
      if (mark) {
        link.href = mark
        link.removeAttribute("type")
      } else {
        link.setAttribute("href", link.dataset.default)
        if (link.dataset.defaultType) link.type = link.dataset.defaultType
      }
    }
  }, [brand.name, brand.mark_url])
}

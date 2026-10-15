import { brandAssetURL, type ApiBrand } from "@/lib/api"
import { MeshMark } from "@/components/layout/mesh-mark"
import { cn } from "@/lib/utils"

/** The brand's square mark, or Meshploy's. */
export function BrandMark({ brand, className }: { brand: ApiBrand; className?: string }) {
  const src = brandAssetURL(brand.mark_url)
  if (src) return <img src={src} alt="" className={cn("object-contain", className)} />
  return <MeshMark className={cn("text-primary", className)} />
}

/** The brand's wordmark, or its name set as Meshploy's is. */
export function BrandWordmark({ brand, className, imageClassName }: { brand: ApiBrand; className?: string; imageClassName?: string }) {
  const src = brandAssetURL(brand.logo_url)
  if (src) return <img src={src} alt={brand.name} className={cn("object-contain", imageClassName)} />
  return <span className={className}>{brand.custom ? brand.name : "meshploy"}</span>
}

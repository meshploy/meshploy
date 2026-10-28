import { brandAssetURL, type ApiBrand } from "@/lib/api"
import { MeshMark } from "@/components/layout/mesh-mark"
import { cn } from "@/lib/utils"

/** The brand's square mark, or Meshploy's. */
export function BrandMark({ brand, className }: { brand: ApiBrand; className?: string }) {
  const src = brandAssetURL(brand.mark_url)
  if (src) return <img src={src} alt="" className={cn("object-contain", className)} />
  return <MeshMark className={cn("text-primary", className)} />
}

/**
 * The brand's wordmark, or its name set as Meshploy's is.
 *
 * Meshploy's own is one lockup on every surface: "meshploy" in the heavy
 * weight, and the product after it in the light one ("meshploy apps",
 * "meshploy cloud"), part of the name rather than a badge. The console is a
 * workspace, the name Cloud and Fleet give every control plane, self-hosted
 * or not.
 */
export function BrandWordmark({
  brand,
  product,
  className,
  imageClassName,
}: {
  brand: ApiBrand
  product?: string
  className?: string
  imageClassName?: string
}) {
  const src = brandAssetURL(brand.logo_url)
  if (src) return <img src={src} alt={brand.name} className={cn("object-contain", imageClassName)} />
  if (brand.custom) return <span className={cn("font-semibold", className)}>{brand.name}</span>
  return (
    <span className={cn("tracking-tight", className)}>
      <span className="font-semibold">meshploy</span>
      {product && <span className="ml-[0.1em] text-muted-foreground">{product}</span>}
    </span>
  )
}

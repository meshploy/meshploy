import { createFileRoute, Outlet } from "@tanstack/react-router"
import { BrandMark, BrandWordmark } from "@/components/layout/brand"
import { useBrand } from "@/lib/brand"
import { cn } from "@/lib/utils"

export const Route = createFileRoute("/_auth")({
  component: AuthLayout,
})

function AuthLayout() {
  const brand = useBrand()
  return (
    <div className="min-h-screen flex items-center justify-center bg-background p-4">
      <div
        className="fixed inset-0 pointer-events-none"
        style={{
          backgroundImage:
            "linear-gradient(oklch(0.24 0 0) 1px, transparent 1px), linear-gradient(90deg, oklch(0.24 0 0) 1px, transparent 1px)",
          backgroundSize: "40px 40px",
          opacity: 0.12,
        }}
      />
      <div className="relative w-full max-w-md space-y-8">
        <div className="flex flex-col items-center gap-3">
          <div className="text-center">
            <h1 className="flex items-center justify-center gap-2 text-2xl">
              {/* Meshploy's mark has empty space at its left edge, drawn back out so the lockup centres on what is seen. */}
              <BrandMark brand={brand} className={cn("size-8 shrink-0", !brand.custom && "-ml-[3.5px]")} />
              <BrandWordmark brand={brand} product="workspace" imageClassName="h-8 max-w-60" />
            </h1>
            {!brand.custom && <p className="text-sm text-muted-foreground mt-0.5">Your infrastructure, in your control.</p>}
          </div>
        </div>
        <Outlet />
      </div>
    </div>
  )
}

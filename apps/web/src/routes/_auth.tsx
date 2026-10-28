import { createFileRoute, Outlet } from "@tanstack/react-router"
import { BrandMark, BrandWordmark } from "@/components/layout/brand"
import { useBrand } from "@/lib/brand"

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
            <h1 className="flex items-center justify-center gap-3 text-2xl">
              <BrandMark brand={brand} className="size-8 shrink-0" />
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

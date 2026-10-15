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
          <div className="flex items-center justify-center w-12 h-12 rounded-2xl bg-primary/15">
            <BrandMark brand={brand} className="w-5 h-5" />
          </div>
          <div className="text-center">
            <h1 className="text-2xl font-semibold tracking-tight">
              <BrandWordmark brand={brand} imageClassName="h-8 max-w-60 mx-auto" />
            </h1>
            {!brand.custom && <p className="text-sm text-muted-foreground mt-0.5">Your infrastructure, in your control.</p>}
          </div>
        </div>
        <Outlet />
      </div>
    </div>
  )
}

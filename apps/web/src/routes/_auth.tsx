import { createFileRoute, Outlet } from "@tanstack/react-router"
import { MeshMark } from "@/components/layout/mesh-mark"

export const Route = createFileRoute("/_auth")({
  component: AuthLayout,
})

function AuthLayout() {
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
            <MeshMark className="w-5 h-5 text-primary" />
          </div>
          <div className="text-center">
            <h1 className="text-2xl font-semibold tracking-tight">meshploy</h1>
            <p className="text-sm text-muted-foreground mt-0.5">Your infrastructure, in your control.</p>
          </div>
        </div>
        <Outlet />
      </div>
    </div>
  )
}

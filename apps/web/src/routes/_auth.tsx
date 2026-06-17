import { createFileRoute, Outlet } from "@tanstack/react-router"

export const Route = createFileRoute("/_auth")({
  component: AuthLayout,
})

function MeshMark({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 100 100" fill="none" stroke="currentColor" strokeWidth="3" strokeLinecap="round" strokeLinejoin="round" className={className}>
      <polyline points="18,78 18,22 50,58 82,22 82,78" />
      <line x1="18" y1="78" x2="50" y2="58" opacity="0.45" />
      <line x1="82" y1="78" x2="50" y2="58" opacity="0.45" />
      <circle cx="18" cy="78" r="5.5" fill="currentColor" stroke="none" />
      <circle cx="18" cy="22" r="5.5" fill="currentColor" stroke="none" />
      <circle cx="50" cy="58" r="5.5" fill="currentColor" stroke="none" />
      <circle cx="82" cy="22" r="5.5" fill="currentColor" stroke="none" />
      <circle cx="82" cy="78" r="5.5" fill="currentColor" stroke="none" />
    </svg>
  )
}

function AuthLayout() {
  return (
    <div className="min-h-screen flex items-center justify-center bg-background p-4">
      <div
        className="fixed inset-0 pointer-events-none"
        style={{
          backgroundImage:
            "linear-gradient(oklch(0.24 0 0) 1px, transparent 1px), linear-gradient(90deg, oklch(0.24 0 0) 1px, transparent 1px)",
          backgroundSize: "40px 40px",
          opacity: 0.3,
        }}
      />
      <div className="relative w-full max-w-sm space-y-8">
        <div className="flex flex-col items-center gap-3">
          <div className="flex items-center justify-center w-10 h-10 rounded-xl bg-primary/15">
            <MeshMark className="w-5 h-5 text-primary" />
          </div>
          <div className="text-center">
            <h1 className="text-lg font-semibold tracking-tight">meshploy</h1>
            <p className="text-sm text-muted-foreground mt-0.5">Internal Developer Platform</p>
          </div>
        </div>
        <Outlet />
      </div>
    </div>
  )
}

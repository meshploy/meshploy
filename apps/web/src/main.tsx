import { StrictMode } from "react"
import { createRoot } from "react-dom/client"
import { RouterProvider, createRouter } from "@tanstack/react-router"
import { QueryCache, QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { routeTree } from "./routeTree.gen"
import { useAuthStore } from "@/store/auth-store"
import "./index.css"

const router = createRouter({ routeTree })

const queryClient = new QueryClient({
  queryCache: new QueryCache({
    onError: (error) => {
      if (error instanceof Error && "status" in error && (error as { status: number }).status === 401) {
        useAuthStore.getState().clearAuth()
        router.navigate({ to: "/login" })
      }
    },
  }),
  defaultOptions: {
    queries: {
      staleTime: 30_000,
      retry: (count, err: unknown) => {
        if (err instanceof Error && "status" in err) {
          const status = (err as { status: number }).status
          if (status === 401 || status === 403) return false
        }
        return count < 2
      },
    },
  },
})

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router
  }
}

async function bootstrap() {
  if (import.meta.env.VITE_DEMO_MODE === "true") {
    const { worker } = await import("./mocks/browser")
    await worker.start({ onUnhandledRequest: "warn" })
    // Signal to Playwright (and any other automation) that MSW is active
    ;(window as Window & { __MSW_READY__?: true }).__MSW_READY__ = true
  }

  const root = document.getElementById("root")!
  createRoot(root).render(
    <StrictMode>
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    </StrictMode>
  )
}

bootstrap()

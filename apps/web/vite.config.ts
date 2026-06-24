import { defineConfig } from "vite"
import react from "@vitejs/plugin-react"
import tailwindcss from "@tailwindcss/vite"
import { TanStackRouterVite } from "@tanstack/router-plugin/vite"
import path from "path"

const isDemo = process.env.VITE_DEMO_MODE === "true"

export default defineConfig({
  plugins: [
    TanStackRouterVite({
      routesDirectory: "./src/routes",
      generatedRouteTree: "./src/routeTree.gen.ts",
    }),
    react(),
    tailwindcss(),
  ],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
  // In demo mode, force relative API paths so MSW service worker can intercept them
  ...(isDemo && {
    define: {
      "import.meta.env.VITE_API_URL": '""',
    },
  }),
})

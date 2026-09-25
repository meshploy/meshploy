import { defineConfig, type Plugin } from "vite"
import { readdirSync, readFileSync } from "fs"
import react from "@vitejs/plugin-react"
import tailwindcss from "@tailwindcss/vite"
import { TanStackRouterVite } from "@tanstack/router-plugin/vite"
import path from "path"

const isDemo = process.env.VITE_DEMO_MODE === "true"

/**
 * The help topics (packages/help/topics/*.md), bundled as virtual:help-topics:
 * one text, read by the console's help drawer, the docs site and the MCP
 * server. HELP_DIR points elsewhere where the repository is not laid out
 * around the build, as in the web image, which receives the folder as a build
 * context of its own.
 */
function helpTopics(): Plugin {
  const dir = process.env.HELP_DIR ?? path.resolve(__dirname, "../../packages/help/topics")
  const id = "virtual:help-topics"
  return {
    name: "help-topics",
    // Watch the folder, not only the files it had at start: a topic added or
    // removed while the dev server runs reloads the console with it.
    configureServer(server) {
      server.watcher.add(dir)
      const reload = (file: string) => {
        if (!file.startsWith(dir)) return
        const mod = server.moduleGraph.getModuleById("\0" + id)
        if (mod) server.moduleGraph.invalidateModule(mod)
        server.ws.send({ type: "full-reload" })
      }
      server.watcher.on("add", reload)
      server.watcher.on("unlink", reload)
      server.watcher.on("change", reload)
    },
    resolveId: (source) => (source === id ? "\0" + id : undefined),
    load(source) {
      if (source !== "\0" + id) return
      const topics: Record<string, string> = {}
      for (const file of readdirSync(dir).filter((f) => f.endsWith(".md")).sort()) {
        this.addWatchFile(path.join(dir, file))
        topics[file.replace(/\.md$/, "")] = readFileSync(path.join(dir, file), "utf8")
      }
      return `export default ${JSON.stringify(topics)}`
    },
  }
}

export default defineConfig({
  plugins: [
    TanStackRouterVite({
      routesDirectory: "./src/routes",
      generatedRouteTree: "./src/routeTree.gen.ts",
    }),
    react(),
    tailwindcss(),
    helpTopics(),
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

# apps/web — Agent rules

## Stack (exact versions)

| Package | Version | Notes |
|---|---|---|
| Vite | 6.x | Build tool — replaces Next.js |
| React | 19.x | All components are client-side |
| TanStack Router | 1.x | File-based routing in `src/routes/` |
| Tailwind CSS | 4.x | CSS-first via `@tailwindcss/vite` — **no tailwind.config.js** |
| shadcn/ui | 4.x (Nova preset) | Components in `src/components/ui/` |
| `@base-ui/react` | 1.x | Replaces Radix UI — **breaking API** |
| Zustand | 5.x | Global + UI state |
| lucide-react | 1.x | Icons |

---

## Critical: @base-ui/react is NOT Radix UI

All shadcn/ui components use `@base-ui/react` primitives, not `@radix-ui/*`.

### No `asChild` prop — use `render` instead

```tsx
// ❌ WRONG
<TooltipTrigger asChild>
  <Link to="/foo" className="..." />
</TooltipTrigger>

// ✅ CORRECT
<TooltipTrigger render={<Link to="/foo" className="..." />}>
  <Icon className="h-4 w-4" />
</TooltipTrigger>
```

For triggers keeping the default `<button>` element, just pass children directly — no `render` prop needed.

### `delayDuration` does not exist on Tooltip

Set delay on `TooltipProvider` in `src/routes/__root.tsx`, not per-tooltip.

---

## Tailwind v4 — CSS-first, no config file

There is **no** `tailwind.config.js/ts`. Tailwind runs via `@tailwindcss/vite` plugin in `vite.config.ts`. All theme tokens are defined in `src/index.css` under `@theme inline { ... }`.

Dark mode uses `@custom-variant dark (&:is(.dark *))`. The `<html>` element has `class="dark"` set in `index.html` — this app is dark-only.

Use `oklch()` throughout. Do not introduce hex or hsl values.

---

## TanStack Router conventions

### File-based routing in `src/routes/`

```
src/routes/
├── __root.tsx                    # Root layout (TooltipProvider, QueryClientProvider)
├── _app.tsx                      # Pathless authenticated layout (sidebar + topbar)
├── _app/
│   ├── index.tsx                 # / → redirects to /nodes via beforeLoad
│   ├── account/index.tsx         # /account — user profile, password, 2FA
│   ├── users/
│   │   ├── index.tsx             # /users — org member list
│   │   └── $userId.tsx           # /users/:userId
│   ├── nodes/
│   │   ├── index.tsx             # /nodes
│   │   └── $id.tsx               # /nodes/:id — node detail + terminal
│   ├── projects/
│   │   ├── index.tsx             # /projects
│   │   ├── new.tsx               # /projects/new
│   │   └── $id/
│   │       ├── route.tsx         # /projects/:id layout (project tab bar)
│   │       ├── index.tsx         # /projects/:id → redirects to services
│   │       ├── new.tsx           # /projects/:id/new — create service/stack/job
│   │       ├── settings.tsx      # /projects/:id/settings
│   │       ├── databases.tsx     # /projects/:id/databases
│   │       ├── routes.tsx        # /projects/:id/routes layout
│   │       ├── pipelines.tsx     # /projects/:id/pipelines (placeholder)
│   │       ├── routes/
│   │       │   ├── index.tsx     # /projects/:id/routes
│   │       │   └── $routeId.tsx  # /projects/:id/routes/:routeId
│   │       ├── services/
│   │       │   ├── index.tsx     # /projects/:id/services
│   │       │   └── $serviceId/
│   │       │       ├── route.tsx         # service layout (tab bar)
│   │       │       ├── index.tsx         # → redirects to overview
│   │       │       ├── overview.tsx      # deployments + metrics overview
│   │       │       ├── config.tsx        # env vars, build config, ports
│   │       │       ├── settings.tsx      # name, image, resource limits
│   │       │       ├── deployments/
│   │       │       │   ├── index.tsx     # deployment history
│   │       │       │   └── $deploymentId.tsx
│   │       │       ├── logs.tsx          # live log stream
│   │       │       ├── pods.tsx          # pod list + pod terminal
│   │       │       ├── backups.tsx       # backup configs + run history
│   │       │       └── permissions.tsx   # resource-level ACL
│   │       ├── stacks/
│   │       │   ├── index.tsx     # /projects/:id/stacks
│   │       │   └── $stackId/
│   │       │       ├── route.tsx         # stack layout
│   │       │       ├── index.tsx         # stack overview
│   │       │       ├── editor.tsx        # compose spec editor
│   │       │       ├── services.tsx      # stack-owned services
│   │       │       ├── variables.tsx     # variable groups for stack
│   │       │       └── permissions.tsx
│   │       ├── jobs/
│   │       │   ├── index.tsx     # /projects/:id/jobs
│   │       │   └── $jobId/
│   │       │       ├── route.tsx         # job layout
│   │       │       ├── index.tsx         # job overview + trigger
│   │       │       ├── config.tsx        # image, command, schedule
│   │       │       ├── runs.tsx          # run history + logs
│   │       │       └── permissions.tsx
│   │       ├── volumes/
│   │       │   ├── index.tsx     # /projects/:id/volumes
│   │       │   └── $volumeId.tsx # volume detail + mounts
│   │       └── variables/
│   │           ├── index.tsx     # /projects/:id/variables — variable group list
│   │           └── $groupId.tsx  # variable group detail + items
│   ├── cluster/index.tsx         # /cluster — Headscale, K3s status, preauth key
│   ├── integrations/
│   │   ├── index.tsx             # /integrations → redirects to /integrations/git
│   │   ├── _tabs.tsx             # pathless layout: header + category tab bar
│   │   ├── _tabs/                # git, registries, storage, notifications, email (sections in components/integrations)
│   │   └── new.tsx               # /integrations/new — add integration wizard
│   └── settings/index.tsx        # /settings — org settings, notifications, SMTP
├── _auth.tsx                     # Pathless auth layout (centered card)
└── _auth/
    ├── login.tsx                 # /login
    └── register.tsx              # /register
```

Underscore-prefixed files (`_app.tsx`, `_auth.tsx`) are **pathless layouts** — they wrap child routes without adding to the URL.

### Every route file must export a `Route` constant

```tsx
export const Route = createFileRoute("/_app/nodes/")({
  component: NodesPage,
})
```

### Loaders instead of server-side data fetching

```tsx
export const Route = createFileRoute("/_app/nodes/$id")({
  loader: ({ params }) => {
    const node = mockNodes.find((n) => n.id === params.id)
    if (!node) throw notFound()
    return { node }
  },
  component: NodeDetailPage,
})

function NodeDetailPage() {
  const { node } = Route.useLoaderData()
  // ...
}
```

### Navigation APIs (replaces Next.js)

```tsx
// Link (replaces next/link)
import { Link } from "@tanstack/react-router"
<Link to="/nodes">Nodes</Link>
<Link to="/nodes/$id" params={{ id: node.id }}>Detail</Link>

// Programmatic navigation (replaces useRouter().push)
import { useNavigate } from "@tanstack/react-router"
const navigate = useNavigate()
navigate({ to: "/nodes/$id", params: { id: node.id } })

// Current pathname (replaces usePathname)
import { useRouterState } from "@tanstack/react-router"
const pathname = useRouterState({ select: (s) => s.location.pathname })

// Redirect in beforeLoad (replaces next/navigation redirect)
throw redirect({ to: "/nodes" })

// Not found (replaces next/navigation notFound)
throw notFound()
```

### Route tree is auto-generated

`src/routeTree.gen.ts` is generated by the `@tanstack/router-plugin/vite` Vite plugin during `vite` / `vite build`. **Never edit it manually.** It regenerates automatically when you add/rename route files.

---

## No Server Components

Everything is client-side React. Ignore `"use client"` directives in `src/components/ui/` — they're shadcn-generated remnants, harmless in Vite. Do not add them to new files.

---

## Dev commands

```bash
npm run dev       # Start Vite dev server (also regenerates routeTree.gen.ts)
npm run build     # tsc type-check + vite build
npm run preview   # Preview the production build
```

---

## Adding shadcn/ui components

```bash
npx shadcn@latest add -d <component>   # -d for non-interactive defaults
```

Components land in `src/components/ui/`. Do not edit them manually.

---

## File structure

```
apps/web/
├── src/
│   ├── routes/             # TanStack Router file-based routes (see route tree above)
│   ├── components/
│   │   ├── layout/         # app-sidebar, topbar, org-switcher, user-menu
│   │   ├── nodes/          # nodes-table, node-status-dot
│   │   ├── services/       # service cards, deploy button, status badges
│   │   ├── stacks/         # stack card, compose editor
│   │   ├── jobs/           # job card, run history table
│   │   ├── projects/       # project card, create dialog
│   │   ├── permissions/    # resource permission grant dialog
│   │   ├── metrics/        # CPU/memory/network charts
│   │   ├── backups/        # backup config form, object list
│   │   ├── domains/        # domain card, DNS verify
│   │   ├── terminal/       # WebSocket terminal component (xterm.js)
│   │   ├── explorer/       # DB explorer query editor + schema tree
│   │   └── ui/             # shadcn/ui (do not edit manually)
│   ├── lib/
│   │   ├── api/            # Typed REST API client (one file per domain)
│   │   ├── mock-data.ts    # Realistic mock data for Storybook / offline dev
│   │   ├── utils.ts        # cn(), formatRelativeTime(), formatBytes()
│   │   ├── accents.ts      # Project colour accent palette
│   │   └── env-lang.ts     # Language detection from file extension
│   ├── store/
│   │   ├── org-store.ts    # Zustand — current org (persisted)
│   │   └── ui-store.ts     # Zustand — sidebar collapsed (persisted)
│   ├── types/index.ts      # Shared TypeScript types
│   ├── index.css           # Tailwind v4 imports + dark theme tokens (oklch)
│   ├── main.tsx            # App entry point + router setup
│   └── routeTree.gen.ts    # Auto-generated — do not edit
├── index.html              # HTML entry point (has class="dark" on <html>)
├── vite.config.ts
├── tsconfig.json
├── tsconfig.app.json
├── tsconfig.node.json
├── components.json         # shadcn/ui config
└── package.json
```

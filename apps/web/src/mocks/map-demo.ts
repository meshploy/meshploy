// A stack with a chain of dependencies and one broken piece, in the demo's
// Experiments project, so the infrastructure map has something to show: a
// database that keeps crashing, and what waits on it.
import * as seed from "./data"

const P = "00000000-0000-0000-0000-000000000040" // Experiments
const STACK = "00000000-0000-0000-0000-0000000005c0"
const id = (n: number) => `00000000-0000-0000-0000-0000000005${String(n).padStart(2, "0")}`

export const SHOP = {
  storefront: id(1), checkout: id(2), worker: id(3), migrate: id(4), redis: id(5), ordersDb: id(6), assets: id(7),
}

const spec = `services:
  storefront:
    build: ./storefront
    ports: ["3000"]
    depends_on: [checkout]
  checkout:
    build: ./checkout
    depends_on:
      orders-db: { condition: service_healthy }
      redis: { condition: service_started }
      migrate: { condition: service_completed_successfully }
  orders-worker:
    build: ./worker
    depends_on: [orders-db, redis]
  migrate:
    build: ./checkout
    command: npm run migrate
    depends_on: [orders-db]
  redis:
    image: redis:7-alpine
  orders-db:
    image: postgres:17
    volumes: [orders-data:/var/lib/postgresql/data]
volumes:
  orders-data:
`

export const shopStack = {
  ...seed.demoStack, id: STACK, project_id: P, name: "shop", spec, status: "running" as const,
}

const app = (sid: string, name: string, over: Record<string, unknown> = {}) => ({
  ...seed.demoServiceApi, id: sid, name, slug: name, project_id: P, stack_id: STACK, node_id: null,
  image: `100.64.0.1:5000/shop-${name}:a1b2c3d`, ports: [{ id: `${sid}-p`, service_id: sid, name: "http", port: 3000, is_http: true, is_primary: true, is_public: false, node_port: 0 }],
  replicas: 1, status: "running" as const, ...over,
})

export const shopServices = [
  app(SHOP.storefront, "storefront", { replicas: 2 }),
  app(SHOP.checkout, "checkout"),
  app(SHOP.worker, "orders-worker", { ports: [] }),
  app(SHOP.migrate, "migrate", { run_once: true, status: "completed" as const, ports: [] }),
  app(SHOP.redis, "redis", { image: "redis:7-alpine", ports: [{ id: "rp", service_id: SHOP.redis, name: "redis", port: 6379, is_http: false, is_primary: true, is_public: false, node_port: 0 }] }),
  app(SHOP.ordersDb, "orders-db", { image: "postgres:17", ports: [{ id: "op", service_id: SHOP.ordersDb, name: "postgres", port: 5432, is_http: false, is_primary: true, is_public: false, node_port: 0 }] }),
  // Not in the stack: a service made on its own, with a failed build.
  { ...app(SHOP.assets, "assets"), stack_id: null, latest_deploy_failed: true },
]

export const shopRoutes = [
  {
    ...seed.demoRoute, id: id(20), project_id: P, subdomain: "shop", hostname: "shop.experiments.example", stack_id: STACK,
    targets: [{ id: id(21), route_id: id(20), service_id: SHOP.storefront, target_port: 3000, weight: 100, path: "/", strip_path: false, node_id: null, target_ip: "", redirect_route_id: null, redirect_code: 301 }],
  },
  {
    ...seed.demoRoute, id: id(22), project_id: P, subdomain: "cdn", hostname: "cdn.experiments.example", stack_id: null,
    targets: [{ id: id(23), route_id: id(22), service_id: SHOP.assets, target_port: 3000, weight: 100, path: "/", strip_path: false, node_id: null, target_ip: "", redirect_route_id: null, redirect_code: 301 }],
  },
]

export const shopVolumes = [
  {
    ...seed.demoVolume, id: id(30), project_id: P, name: "shop-orders-data", slug: "shop-orders-data", stack_id: STACK, storage_gb: 20,
    mounts: [{ id: id(31), volume_id: id(30), service_id: SHOP.ordersDb, mount_path: "/var/lib/postgresql/data", created_at: seed.DEMO_NOW, updated_at: seed.DEMO_NOW }],
  },
]

/** orders-db keeps crashing: out of memory, as a database with too small a limit does. */
export const shopTroubles: Record<string, Record<string, unknown>> = {
  [SHOP.ordersDb]: { kind: "out_of_memory", restarts: 14, last_at: seed.DEMO_NOW, exit_code: 137, memory_limit: "256Mi" },
}

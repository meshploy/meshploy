# Meshploy Monorepo Architecture & Rules

This is a Go Workspaces monorepo combining a Go control plane with a Next.js frontend, deployed via Docker Compose.

## 🏗️ Architecture
- `apps/api`: Chi + Huma REST API (Go, OpenAPI 3.1).
- `apps/proxy`: Dynamic reverse proxy using standard net/http (Go).
- `apps/web`: Next.js App Router frontend.
- `packages/db`: Shared GORM SQLite models imported by both Go apps.
- `deploy/`: Infrastructure layer (Headscale, CoreDNS, Docker Compose).

## 💻 Commands
- **Run API (Dev):** `cd apps/api && go run main.go`
- **Run Proxy (Dev):** `cd apps/proxy && go run main.go`
- **Run Web (Dev):** `cd apps/web && npm run dev`
- **Database Sync:** GORM `AutoMigrate` runs automatically on API startup.
- **Run Infra:** `cd deploy && docker compose -f docker-compose.dev.yml up -d`

## 📝 Coding Standards
### Go (API & Proxy & DB)
- Use Go 1.22+ syntax.
- Use `github.com/google/uuid` for all primary keys in `packages/db`.
- Return standard JSON error responses via Huma's RFC 7807 problem details in `apps/api`.
- NEVER write business logic directly in HTTP handlers; abstract it.

### TypeScript (Next.js)
- Strictly use the App Router (`app/` directory).
- Default to Server Components; use `'use client'` only when React hooks/state are required.
- Use Tailwind CSS and `shadcn/ui` for styling.

## ⚠️ Safety Guardrails
- NEVER modify or delete files inside `deploy/headscale/data/`.
- NEVER commit `.db`, `.db-shm`, or `.db-wal` files.
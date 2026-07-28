# Extension points

The sockets a private module (`meshploy-ee`, and `meshploy-ee-<client>` above it)
plugs into. There is no central registry: each hook lives in the package it
extends, mirroring the `db.RegisterMigration` pattern that predates them.

A central `packages/extension` registry was designed and rejected — the service
layer must *invoke* the quota hook while the registry would need service types
for its dependency struct, producing a `service → extension → service` import
cycle. Breaking it would have forced narrow interfaces, which is exactly the
limitation the `packages/server` move existed to remove.

**CE guarantee:** the CE binary never imports the EE module, so every hook slice
below is empty in a CE build. Each hook has a test asserting this.

## The five hooks

| Hook | Package | Purpose |
|---|---|---|
| `db.RegisterMigration(fn)` | `packages/db` | Schema — EE tables |
| `handler.RegisterRoutes(fn)` | `packages/server/handler` | Additional HTTP endpoints |
| `server.RegisterMiddleware(priority, mw)` | `packages/server` | HTTP middleware |
| `service.RegisterQuotaChecker(qc)` | `packages/server/service` | Per-org resource limits |
| `k8s.RegisterJobMutator(jm)` | `packages/server/k8s` | Mutate Jobs before submission |

All are called from an extension package's `init()`.

## Routes

```go
func init() { handler.RegisterRoutes(routes) }

func routes(api huma.API, h *handler.Handler) {
    huma.Register(api, huma.Operation{ /* ... */ }, myHandler(h.Services()))
}
```

Callbacks run **after** all CE routes are mounted, so an extension can rely on
the CE surface existing. `h.Services()` and `h.Config()` expose the injected
dependencies, which are otherwise unexported fields.

## Middleware

Ordering is explicit rather than registration-order because it is
security-relevant: audit logging registered before authentication would record
every action as anonymous.

```go
func init() { server.RegisterMiddleware(server.PriorityAfterAuth, auditWrites) }
```

| Priority | Runs | Use for |
|---|---|---|
| `PriorityBeforeAuth` | before the principal is resolved | transport concerns only — the caller is unknown |
| `PriorityAfterAuth` | after auth and org-membership checks | audit logging, anything needing the caller |

Ties break on registration order. A middleware appears in exactly one phase.

## Quotas

CE ships no quotas. EE's MSP mode registers a checker so one install can host
many orgs with enforced limits — the same machinery Meshploy Cloud reuses.

```go
func init() { service.RegisterQuotaChecker(orgQuotas{}) }

func (orgQuotas) CheckQuota(ctx context.Context, orgID uuid.UUID, kind service.QuotaKind) error {
    // return a user-facing error to block, e.g. "project limit reached (10)"
}
```

Invoked at three create paths: `QuotaProject` (`ProjectService.Create`),
`QuotaService` (`WorkloadService.Create`, before the database branch so both
service kinds are metered), and `QuotaNode` (`NodeService.Register`).

**Gateway nodes are not metered.** Only workers count — the gateway is fixed
overhead, and counting it would mean buying the HA-gateway feature consumes the
customer's node allowance. The first rejection wins; the error propagates
verbatim so the user sees the extension's message.

Add a `QuotaKind` when a feature needs one; the type is a string, not an enum,
so an extension may meter kinds CE does not know about.

## Job mutation

Lives in `k8s` rather than `service` because the Job object is built here.
`CreateBuildJob` and `CreateRunJob` are the only two places a `batchv1.Job` is
constructed, and a mutator must see the final object immediately before
submission — hooking at the service layer would miss one and observe an unbuilt
spec.

```go
func init() { k8s.RegisterJobMutator(buildIsolation{}) }

func (buildIsolation) MutateJob(ctx context.Context, kind k8s.JobKind, job *batchv1.Job) error {
    if kind != k8s.JobBuild { return nil }
    // rewrite namespace, attach NetworkPolicy labels, ...
}
```

Mutators run in registration order on the final object and may modify it in
place. **Returning an error aborts submission** — build isolation fails closed
rather than letting an unisolated Job run.

`ApplyCronJob` is deliberately not covered: it submits a `batchv1.CronJob`, a
different type whose pod template sits at `Spec.JobTemplate.Spec`. When a feature
needs cron isolation, add a sibling `CronJobMutator` rather than widening this
interface to lie about the type.

## Adding a hook

Only when a feature needs it. Deliberately not built: a generic string-keyed
event bus (untyped, unbounded), speculative hooks for unwritten features, and
dynamic plugin loading — Go's `plugin` package requires identical build flags,
breaks across versions, and has no Windows support, which is why EE composes at
compile time as a separate binary.

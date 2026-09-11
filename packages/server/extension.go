package server

import (
	"net/http"
	"sort"

	"github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/handler"
	"github.com/meshploy/packages/server/k8s"
	"github.com/meshploy/packages/server/service"
	"github.com/meshploy/packages/server/version"
)

// detectEdition reports which edition this binary is: Enterprise when anything
// is registered in any of the five extension hooks, Community otherwise.
//
// Every Enterprise build registers something from init(), and the Community
// binary never imports a package that does. Main calls this once, after every
// init() has run.
func detectEdition() string {
	registered := len(middlewareHooks) +
		handler.RoutesRegistered() +
		service.QuotaCheckersRegistered() +
		k8s.JobMutatorsRegistered() +
		db.MigrationsRegistered()
	if registered > 0 {
		return version.EditionEnterprise
	}
	return version.EditionCommunity
}

// Extension point: additional HTTP middleware.
//
// Ordering is explicit rather than registration-order because it is
// security-relevant: audit logging registered before authentication would
// record every action as anonymous. Extensions state their phase; ties break on
// registration order.
const (
	// PriorityBeforeAuth runs before the principal is resolved. Use only for
	// transport concerns (tracing, request shaping) — the caller is unknown here.
	PriorityBeforeAuth = 100

	// PriorityAfterAuth runs once the principal is in context and org membership
	// has been checked. This is the correct phase for audit logging and anything
	// that needs to know who is calling.
	PriorityAfterAuth = 200
)

type extMiddleware struct {
	priority int
	seq      int // registration order, for stable ties
	mw       func(http.Handler) http.Handler
}

// middlewareHooks stays empty in CE builds — the CE binary never imports EE.
var middlewareHooks []extMiddleware

// RegisterMiddleware adds HTTP middleware at the given priority. Call from an
// extension package's init():
//
//	func init() { server.RegisterMiddleware(server.PriorityAfterAuth, auditWrites) }
func RegisterMiddleware(priority int, mw func(http.Handler) http.Handler) {
	middlewareHooks = append(middlewareHooks, extMiddleware{
		priority: priority,
		seq:      len(middlewareHooks),
		mw:       mw,
	})
}

// middlewareFor returns the registered middleware for one phase, ordered by
// priority then registration order. A phase's middleware is everything with
// priority <= that phase and > the preceding phase.
func middlewareFor(low, high int) []func(http.Handler) http.Handler {
	var picked []extMiddleware
	for _, m := range middlewareHooks {
		if m.priority > low && m.priority <= high {
			picked = append(picked, m)
		}
	}
	sort.SliceStable(picked, func(i, j int) bool {
		if picked[i].priority != picked[j].priority {
			return picked[i].priority < picked[j].priority
		}
		return picked[i].seq < picked[j].seq
	})
	out := make([]func(http.Handler) http.Handler, len(picked))
	for i, m := range picked {
		out[i] = m.mw
	}
	return out
}

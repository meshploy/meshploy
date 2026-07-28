package server

import (
	"net/http"
	"testing"
)

// The middleware extension point is ordering-sensitive: audit logging that runs
// before authentication silently records every action as anonymous. These tests
// pin the ordering contract rather than trusting registration order.

func TestMiddlewareForSelectsByPhase(t *testing.T) {
	saved := middlewareHooks
	t.Cleanup(func() { middlewareHooks = saved })
	middlewareHooks = nil

	noop := func(http.Handler) http.Handler { return nil }
	RegisterMiddleware(PriorityBeforeAuth, noop)
	RegisterMiddleware(PriorityAfterAuth, noop)

	if got := len(middlewareFor(0, PriorityBeforeAuth)); got != 1 {
		t.Fatalf("pre-auth phase: want 1 middleware, got %d", got)
	}
	if got := len(middlewareFor(PriorityBeforeAuth, PriorityAfterAuth)); got != 1 {
		t.Fatalf("post-auth phase: want 1 middleware, got %d", got)
	}
}

// A middleware must appear in exactly one phase — double-application would run
// audit logging (or any extension) twice per request.
func TestMiddlewarePhasesDoNotOverlap(t *testing.T) {
	saved := middlewareHooks
	t.Cleanup(func() { middlewareHooks = saved })
	middlewareHooks = nil

	noop := func(http.Handler) http.Handler { return nil }
	RegisterMiddleware(PriorityAfterAuth, noop)

	if got := len(middlewareFor(0, PriorityBeforeAuth)); got != 0 {
		t.Fatalf("an after-auth middleware must not appear in the pre-auth phase, got %d", got)
	}
	if got := len(middlewareFor(PriorityBeforeAuth, PriorityAfterAuth)); got != 1 {
		t.Fatalf("after-auth phase: want 1, got %d", got)
	}
}

// Ties break on registration order so extension authors get a predictable chain.
func TestMiddlewareStableOrderWithinPriority(t *testing.T) {
	saved := middlewareHooks
	t.Cleanup(func() { middlewareHooks = saved })
	middlewareHooks = nil

	var order []string
	mk := func(name string) func(http.Handler) http.Handler {
		return func(next http.Handler) http.Handler {
			order = append(order, name)
			return next
		}
	}
	RegisterMiddleware(PriorityAfterAuth, mk("first"))
	RegisterMiddleware(PriorityAfterAuth, mk("second"))

	for _, mw := range middlewareFor(PriorityBeforeAuth, PriorityAfterAuth) {
		mw(nil)
	}
	if len(order) != 2 || order[0] != "first" || order[1] != "second" {
		t.Fatalf("want [first second] in registration order, got %v", order)
	}
}

// The load-bearing CE guarantee: with nothing registered, every phase is empty.
func TestNoMiddlewareRegisteredInCE(t *testing.T) {
	saved := middlewareHooks
	t.Cleanup(func() { middlewareHooks = saved })
	middlewareHooks = nil

	if got := len(middlewareFor(0, PriorityAfterAuth)); got != 0 {
		t.Fatalf("a CE build must register no extension middleware, got %d", got)
	}
}

package proxy

import (
	"log"
	"net/http"
	"sync"

	"github.com/google/uuid"
)

// Target is the route a request matched, as a gate sees it.
type Target struct {
	Host      string
	RouteID   uuid.UUID
	ServiceID uuid.UUID // zero for a node or address target
	ProjectID uuid.UUID
	OrgID     uuid.UUID
}

// A Gate sees a request before it reaches its app. It may answer it (a
// redirect to sign in, a page) and return true, or return false to let it
// through. Gates run in the order registered; the first to answer wins.
//
// Community registers none, so the proxy serves every route as it always has.
// A build that adds one (Enterprise sign-in, later wake-on-request) registers
// it from an init() before Main runs.
type Gate func(w http.ResponseWriter, r *http.Request, t Target) (answered bool)

var (
	gatesMu sync.RWMutex
	gates   []Gate
)

// RegisterGate adds a gate after the ones already registered.
func RegisterGate(g Gate) {
	gatesMu.Lock()
	defer gatesMu.Unlock()
	gates = append(gates, g)
}

// runGates reports whether a gate answered the request. A gate that panics has
// answered it with a 502: a gate that cannot decide never lets a request
// through, since the app behind it may be one only some people may see.
func runGates(w http.ResponseWriter, r *http.Request, t Target) bool {
	gatesMu.RLock()
	registered := gates
	gatesMu.RUnlock()
	for _, g := range registered {
		if callGate(g, w, r, t) {
			return true
		}
	}
	return false
}

func callGate(g Gate, w http.ResponseWriter, r *http.Request, t Target) (answered bool) {
	defer func() {
		if p := recover(); p != nil {
			log.Printf("proxy: gate panicked for %s: %v", t.Host, p)
			http.Error(w, `{"error":"gate unavailable"}`, http.StatusBadGateway)
			answered = true
		}
	}()
	return g(w, r, t)
}

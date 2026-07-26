package client_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

const (
	testOrg     = "org-1"
	testProject = "proj-1"
	testService = "svc-1"
)

type routeHandler struct {
	method string
	path   string
	fn     http.HandlerFunc
}

// newServer builds a test server that dispatches to the first matching handler.
func newServer(t *testing.T, routes []routeHandler) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, h := range routes {
			if r.Method == h.method && r.URL.Path == h.path {
				h.fn(w, r)
				return
			}
		}
		t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		http.Error(w, "not found", http.StatusNotFound)
	}))
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

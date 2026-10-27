package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// A gate refuses a signed-in principal before any handler runs, with its
// reason as the answer, and never stands in front of a public route.
func TestARequestGateRefusesBeforeTheHandler(t *testing.T) {
	before := requestGates
	t.Cleanup(func() { requestGates = before })
	refused := uuid.New()
	RegisterRequestGate(func(r *http.Request, userID uuid.UUID) error {
		if userID == refused {
			return errors.New("this account opens the Apps face only")
		}
		return nil
	})

	reached := false
	h := RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached = true }))
	serve := func(user *uuid.UUID, path string) *httptest.ResponseRecorder {
		reached = false
		r := httptest.NewRequest(http.MethodGet, path, nil)
		if user != nil {
			r = r.WithContext(context.WithValue(r.Context(), userIDKey, *user))
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}

	allowed := uuid.New()
	if w := serve(&allowed, "/api/v1/orgs"); w.Code != http.StatusOK || !reached {
		t.Errorf("a principal the gate lets through: %d, handler reached %v", w.Code, reached)
	}
	w := serve(&refused, "/api/v1/orgs")
	if w.Code != http.StatusForbidden || reached || !strings.Contains(w.Body.String(), "Apps face only") {
		t.Errorf("a refused principal: %d %q, handler reached %v", w.Code, w.Body.String(), reached)
	}
	if w := serve(nil, "/health"); w.Code != http.StatusOK || !reached {
		t.Errorf("a public route: %d, handler reached %v", w.Code, reached)
	}
}

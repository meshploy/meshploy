package handler

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/meshploy/packages/proxy/edge"
	"github.com/meshploy/packages/server/service"
)

// The edge's own endpoints, for a proxy that has no database: it asks for its
// snapshot and reports its TCP ports, as the node holding the cluster agent,
// with that node's secret. Public to the auth middleware (no person signs
// in); the secret is checked here.

// edgeCaller authenticates the edge, answering 401 itself when it is not.
func (h *Handler) edgeCaller(w http.ResponseWriter, r *http.Request) bool {
	secret, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	id, _ := uuid.Parse(r.Header.Get(edge.NodeHeader))
	err := h.svc.Edge.Authenticate(r.Context(), id, strings.TrimSpace(secret))
	if errors.Is(err, service.ErrNotEdge) {
		http.Error(w, "not this cluster's edge", http.StatusUnauthorized)
		return false
	}
	if err != nil {
		log.Printf("edge: authenticating: %v", err)
		http.Error(w, "could not check the edge's credentials", http.StatusInternalServerError)
		return false
	}
	return true
}

// EdgeSnapshot is GET /api/v1/edge/snapshot. With routes and mesh (the
// versions the edge holds) it waits up to wait seconds for them to move and
// answers 204 when they did not.
func (h *Handler) EdgeSnapshot(w http.ResponseWriter, r *http.Request) {
	if !h.edgeCaller(w, r) {
		return
	}
	q := r.URL.Query()
	var have *edge.Versions
	if q.Has("routes") || q.Has("mesh") {
		routes, err1 := strconv.ParseInt(q.Get("routes"), 10, 64)
		mesh, err2 := strconv.ParseInt(q.Get("mesh"), 10, 64)
		if err1 != nil || err2 != nil {
			http.Error(w, "routes and mesh must both be versions", http.StatusBadRequest)
			return
		}
		have = &edge.Versions{Routes: routes, Mesh: mesh}
	}
	wait := 0 * time.Second
	if v := q.Get("wait"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			http.Error(w, "wait is a number of seconds", http.StatusBadRequest)
			return
		}
		wait = min(time.Duration(n)*time.Second, edge.MaxWait)
	}
	s, err := h.svc.Edge.Snapshot(r.Context(), have, wait)
	if err != nil {
		if r.Context().Err() == nil {
			log.Printf("edge: snapshot: %v", err)
			http.Error(w, "could not read the snapshot", http.StatusInternalServerError)
		}
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if s == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s)
}

// EdgeTCPStatus is POST /api/v1/edge/tcp-status: what became of TCP routes on
// the edge.
func (h *Handler) EdgeTCPStatus(w http.ResponseWriter, r *http.Request) {
	if !h.edgeCaller(w, r) {
		return
	}
	var body struct {
		Routes []edge.TCPStatus `json:"routes"`
	}
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		http.Error(w, "the body is not a status report", http.StatusBadRequest)
		return
	}
	if err := h.svc.Edge.RecordTCP(r.Context(), body.Routes); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

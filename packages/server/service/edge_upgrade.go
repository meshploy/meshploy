package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/meshploy/packages/hostagent"
)

// ErrNoEdge is an edge upgrade asked of an API whose cluster is its own
// machine's, which upgrades with the server's own upgrade.
var ErrNoEdge = errors.New("this server reaches no edge through a cluster agent: it upgrades with the server's own upgrade")

// EdgeUpgrade is how upgrading the edge of an agent-reached cluster stands:
// the edge runs on another machine, whose updater does the work and reports
// through its host agent.
type EdgeUpgrade struct {
	// Pending is a request the edge's host agent has not taken yet.
	Pending bool `json:"pending"`
	// Request is the request asked about, once the host agent took it: queued
	// for the updater, or refused with the reason.
	Request *hostagent.RequestStatus `json:"request,omitempty"`
	// Last is the updater's last upgrade, as the edge reports it.
	Last *hostagent.UpgradeProgress `json:"last,omitempty"`
}

// GetEdgeUpgrade reports the edge's upgrade, and the request with that id
// when one is named. The server's owner only.
func (s *SystemService) GetEdgeUpgrade(ctx context.Context, userID uuid.UUID, requestID string) (EdgeUpgrade, error) {
	host, err := s.edgeHost(ctx, userID)
	if err != nil {
		return EdgeUpgrade{}, err
	}
	var out EdgeUpgrade
	pending, err := hostagent.PendingRequests(host)
	if err != nil {
		return out, err
	}
	for _, req := range pending {
		out.Pending = out.Pending || req.Type == hostagent.RequestServerUpgrade
	}
	if requestID != "" {
		if out.Request, err = hostagent.ReadRequestStatus(host, requestID); err != nil {
			return out, err
		}
	}
	out.Last, err = hostagent.ReadUpgrade(host)
	return out, err
}

// RequestEdgeUpgrade asks the edge to upgrade to the latest build on channel
// ("stable" or "edge"; empty is the edge's own), as Meshploy does release by
// release for the edges it runs the platform of. The server's owner only.
func (s *SystemService) RequestEdgeUpgrade(ctx context.Context, userID uuid.UUID, channel string) (string, error) {
	switch channel {
	case "", "stable", "edge":
	default:
		return "", ErrUnknownChannel
	}
	st, err := s.GetEdgeUpgrade(ctx, userID, "")
	if err != nil {
		return "", err
	}
	if st.Pending || (st.Last != nil && st.Last.State == "running") {
		return "", ErrUpgradeRunning
	}
	req := hostagent.Request{ID: uuid.NewString(), Type: hostagent.RequestServerUpgrade,
		RequestedBy: userID.String(), RequestedAt: time.Now().UTC()}
	if channel != "" {
		req.Args = map[string]string{"channel": channel}
	}
	if err := hostagent.QueueRequest(s.host(), req); err != nil {
		return "", fmt.Errorf("ask the edge to upgrade: %w", err)
	}
	log.Printf("upgrade: user %s asked the edge to upgrade (request %s)", userID, req.ID)
	return req.ID, nil
}

// edgeHost is the edge's host folder, for the server's owner on an API that
// reaches its cluster through an agent.
func (s *SystemService) edgeHost(ctx context.Context, userID uuid.UUID) (hostagent.Folder, error) {
	owner, err := s.IsInstanceOwner(ctx, userID)
	if err != nil {
		return nil, err
	}
	if !owner {
		return nil, ErrNotInstanceOwner
	}
	if s.cfg == nil || !s.cfg.ClusterAgent || s.host() == nil {
		return nil, ErrNoEdge
	}
	return s.host(), nil
}

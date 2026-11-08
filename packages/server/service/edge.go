package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/meshploy/packages/db"
	"github.com/meshploy/packages/proxy/edge"
)

// EdgeService hands the edge its snapshot, on an API that reaches its cluster
// through a cluster agent: the edge's proxy then has no database, and asks
// here instead, as the node that holds the cluster agent.
type EdgeService struct {
	db      *gorm.DB
	enabled bool
	// poll is how often a held request reads the versions again.
	poll time.Duration
}

func NewEdgeService(gdb *gorm.DB, clusterAgent bool) *EdgeService {
	return &EdgeService{db: gdb, enabled: clusterAgent, poll: db.EdgeCheckEvery}
}

// ErrNotEdge is a caller that is not this cluster's edge: a wrong or missing
// secret, a node without the cluster agent, one being removed, or an API that
// has no cluster agent at all. Callers are told nothing more.
var ErrNotEdge = errors.New("not this cluster's edge")

// Authenticate checks the caller is the node holding the cluster agent,
// presenting the secret it registered with.
func (s *EdgeService) Authenticate(ctx context.Context, nodeID uuid.UUID, secret string) error {
	if !s.enabled || nodeID == uuid.Nil || !strings.HasPrefix(secret, "mnode-") {
		return ErrNotEdge
	}
	var node db.Node
	err := s.db.WithContext(ctx).
		Select("id", "cluster_agent_cert", "removal_requested_at").
		Where("id = ? AND node_secret_hash = ?", nodeID, hashToken(secret)).
		Take(&node).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrNotEdge
	}
	if err != nil {
		return err
	}
	if !node.HasClusterAgent() || node.RemovalRequestedAt != nil {
		return ErrNotEdge
	}
	return nil
}

// Snapshot is the edge's snapshot. With have, it waits up to wait for the
// versions to move past have and returns nil when they did not; without, it
// returns at once.
func (s *EdgeService) Snapshot(ctx context.Context, have *edge.Versions, wait time.Duration) (*edge.Snapshot, error) {
	if have != nil {
		deadline := time.Now().Add(wait)
		for {
			v, err := edge.ReadVersions(ctx, s.db)
			if err != nil {
				return nil, err
			}
			if v != *have {
				break
			}
			if !time.Now().Before(deadline) {
				return nil, nil
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(s.poll):
			}
		}
	}
	return edge.Load(ctx, s.db)
}

// RecordTCP keeps what the edge managed to do with TCP routes, as a proxy
// beside the database writes it itself. Only the status and its error are
// written; an unknown route is skipped.
func (s *EdgeService) RecordTCP(ctx context.Context, statuses []edge.TCPStatus) error {
	for _, st := range statuses {
		switch st.Status {
		case db.TCPRouteOpen, db.TCPRoutePaused, db.TCPRouteFailed:
		default:
			return errors.New("unknown TCP route status " + string(st.Status))
		}
		lastErr := st.LastError
		if len(lastErr) > 500 {
			lastErr = lastErr[:500]
		}
		if err := s.db.WithContext(ctx).Model(&db.TCPRoute{}).Where("id = ?", st.ID).
			Updates(map[string]any{"status": st.Status, "last_error": lastErr}).Error; err != nil {
			return err
		}
	}
	return nil
}

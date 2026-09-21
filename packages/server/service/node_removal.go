package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
	appk8s "github.com/meshploy/packages/server/k8s"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

// Removing a node takes three steps, in this order: Headscale drops the node's
// peer, Kubernetes drops the node, and the record goes. Headscale can be
// unreachable, and while it still has the peer the machine can come back,
// rejoin the mesh and re-register with K3s. So a removal Headscale has not
// confirmed is recorded rather than half done, and finished in the background.

// ErrGatewayNode refuses removing the node that runs the control plane.
var ErrGatewayNode = errors.New("the gateway node cannot be removed")

// ErrNoPendingRemoval is CancelRemoval's answer when no removal is waiting.
var ErrNoPendingRemoval = errors.New("this node has no removal waiting")

// RemovalResult reports how far a removal got.
type RemovalResult struct {
	Removed bool   // false while it waits for Headscale; Error says why
	Error   string // why it is waiting
}

// removalRetries are the pauses before each attempt Remove makes at Headscale
// before leaving the rest to the background worker: enough to ride out a
// restart without holding the request for long.
var removalRetries = []time.Duration{0, time.Second, 2 * time.Second}

// Remove removes a node from Headscale, the cluster and Meshploy. Calling it
// again is safe, and is how a removal that is waiting is retried at once.
func (s *NodeService) Remove(ctx context.Context, nodeID uuid.UUID) (*RemovalResult, error) {
	var node db.Node
	if err := s.db.WithContext(ctx).First(&node, "id = ?", nodeID).Error; err != nil {
		return nil, err
	}
	if node.K3sRole == db.K3sRoleServer {
		return nil, ErrGatewayNode
	}
	if node.RemovalRequestedAt == nil {
		now := time.Now()
		if err := s.db.WithContext(ctx).Model(&node).Update("removal_requested_at", now).Error; err != nil {
			return nil, err
		}
		node.RemovalRequestedAt = &now
	}
	return s.finishRemoval(ctx, &node, removalRetries)
}

// CancelRemoval stops a removal that is waiting. Nothing was deleted while it
// waited, so the node carries on as it was.
func (s *NodeService) CancelRemoval(ctx context.Context, nodeID uuid.UUID) error {
	res := s.db.WithContext(ctx).Model(&db.Node{}).
		Where("id = ? AND removal_requested_at IS NOT NULL", nodeID).
		Updates(map[string]any{"removal_requested_at": nil, "removal_error": ""})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNoPendingRemoval
	}
	return nil
}

// StartRemovalWorker finishes waiting removals once a minute.
func (s *NodeService) StartRemovalWorker(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.FinishPendingRemovals(ctx)
		}
	}
}

// FinishPendingRemovals makes one attempt at each removal still waiting.
func (s *NodeService) FinishPendingRemovals(ctx context.Context) {
	var pending []db.Node
	if err := s.db.WithContext(ctx).Where("removal_requested_at IS NOT NULL").Find(&pending).Error; err != nil {
		log.Printf("warning: list pending node removals: %v", err)
		return
	}
	for i := range pending {
		res, err := s.finishRemoval(ctx, &pending[i], []time.Duration{0})
		switch {
		case err != nil:
			log.Printf("warning: remove node %s: %v", pending[i].Name, err)
		case res.Removed:
			log.Printf("node %s removed once Headscale dropped its peer", pending[i].Name)
		}
	}
}

// finishRemoval drops the node's Headscale peer, one attempt after each pause
// in waits, then deletes the node from the cluster and the database. Until
// Headscale confirms, the node stays, with the reason recorded.
func (s *NodeService) finishRemoval(ctx context.Context, node *db.Node, waits []time.Duration) (*RemovalResult, error) {
	if err := s.dropPeer(ctx, node, waits); err != nil {
		return s.waiting(ctx, node, fmt.Sprintf("Headscale has not removed the node's peer: %v", err))
	}
	if s.k8s != nil && node.Name != "" && node.MeshRole != db.MeshRoleMesh {
		// Not fatal: without its peer the machine cannot reach the cluster, so
		// a node object left behind only shows as NotReady. A mesh-only node
		// never had one.
		if err := appk8s.DeleteNode(ctx, s.k8s, node.Name); err != nil && !apierrors.IsNotFound(err) {
			log.Printf("warning: delete k8s node %s: %v", node.Name, err)
		}
	}
	// Ports that forwarded to this node, before the row goes and takes the link
	// with it.
	if err := s.withdrawTCPRoutes(ctx, node); err != nil {
		return s.waiting(ctx, node, fmt.Sprintf("The ports forwarding to this node could not be withdrawn: %v", err))
	}
	// Only a removal still requested: one cancelled meanwhile keeps its node.
	res := s.db.WithContext(ctx).Where("removal_requested_at IS NOT NULL").Delete(&db.Node{}, "id = ?", node.ID)
	if res.Error != nil {
		// Recorded and reported, not returned: a removal that cannot finish is
		// a removal still waiting, and the operator needs to read why. Returned
		// as an error it became "unexpected error occurred" in the console,
		// under a banner blaming Headscale - while the real reason, something
		// still pointing at this node, was only in the server's log.
		return s.waiting(ctx, node, fmt.Sprintf("The node could not be removed: %v", res.Error))
	}
	if res.RowsAffected == 0 {
		return &RemovalResult{Error: "the removal was cancelled"}, nil
	}
	return &RemovalResult{Removed: true}, nil
}

// withdrawTCPRoutes takes the gateway off the ports that forwarded to this node.
//
// Such a port has nowhere to go once the node is gone: the gateway would keep
// listening and hand every connection to an address that answers nothing, or -
// worse - to whatever machine is given that mesh address next. So the route is
// paused, which is what makes the gateway close the listener, and its target is
// cleared.
//
// Paused, not deleted. The port number, the zone and the allowlist are the
// operator's decisions, and they are still good: pointing the route at another
// node is one edit, where recreating it is a small archaeology. The reason is
// written where the console shows it, because a port that stops answering
// without saying why is the worst of both.
func (s *NodeService) withdrawTCPRoutes(ctx context.Context, node *db.Node) error {
	q := s.db.WithContext(ctx).Model(&db.TCPRoute{}).Where("node_id = ?", node.ID)
	if node.TailscaleIP != "" {
		// And any route aimed at its address by hand: a mesh address outlives
		// the machine that held it and can be handed to another, so a route
		// left pointing at one would quietly forward somewhere new.
		q = q.Or("node_id IS NULL AND target_ip = ?", node.TailscaleIP)
	}
	reason := fmt.Sprintf("%s was removed, so this port had nowhere to forward to. "+
		"Point it at another node or a service, then publish it again.", nodeLabel(node))
	return q.Updates(map[string]any{
		"published":   false,
		"status":      db.TCPRoutePaused,
		"target_ip":   "",
		"target_port": 0,
		"last_error":  reason,
	}).Error
}

// nodeLabel is what to call a node in something a person reads.
func nodeLabel(node *db.Node) string {
	if node.Name != "" {
		return node.Name
	}
	if node.TailscaleIP != "" {
		return "the node at " + node.TailscaleIP
	}
	return "the node"
}

// waiting records why a removal has not finished and reports it as a removal
// still in progress. The node stays, so the machine cannot rejoin unnoticed,
// and the reason is on the node for the console to show.
func (s *NodeService) waiting(ctx context.Context, node *db.Node, msg string) (*RemovalResult, error) {
	if err := s.db.WithContext(ctx).Model(node).Update("removal_error", msg).Error; err != nil {
		return nil, err
	}
	return &RemovalResult{Error: msg}, nil
}

// dropPeer removes the node's Headscale peer, trying once after each pause in
// waits and stopping at the first success.
func (s *NodeService) dropPeer(ctx context.Context, node *db.Node, waits []time.Duration) error {
	if s.headscale == nil {
		return nil // no mesh control server is configured, so there is no peer
	}
	if len(waits) == 0 {
		waits = []time.Duration{0}
	}
	var err error
	for _, wait := range waits {
		if wait > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(wait):
			}
		}
		if err = s.tryDropPeer(ctx, node); err == nil {
			return nil
		}
	}
	return err
}

// tryDropPeer makes one attempt. The peer is the one recorded for the node or,
// when none was, the one holding its mesh IP, unless another node claims that
// peer or address. A peer Headscale no longer has counts as dropped, so a
// retry after a partial success finishes cleanly.
func (s *NodeService) tryDropPeer(ctx context.Context, node *db.Node) error {
	id := node.HeadscaleID
	if id == "" {
		if node.TailscaleIP == "" {
			return nil
		}
		peers, err := s.headscale.ListNodes(ctx)
		if err != nil {
			return err
		}
		for _, p := range peers {
			if slices.Contains(p.IPAddresses, node.TailscaleIP) {
				id = p.ID
				break
			}
		}
		if id == "" {
			return nil // no peer holds the node's mesh IP
		}
		// An address can be handed on: never drop a peer another node claims.
		var claimed int64
		if err := s.db.WithContext(ctx).Model(&db.Node{}).
			Where("id <> ? AND (headscale_id = ? OR tailscale_ip = ?)", node.ID, id, node.TailscaleIP).
			Count(&claimed).Error; err != nil {
			return err
		}
		if claimed > 0 {
			return nil
		}
	}
	err := s.headscale.DeleteNode(ctx, id)
	if err == nil || errors.Is(err, ErrHeadscaleNodeNotFound) {
		return nil
	}
	// Some Headscale versions answer a missing node with a server error, so
	// look before calling it a failure.
	if peers, lerr := s.headscale.ListNodes(ctx); lerr == nil &&
		!slices.ContainsFunc(peers, func(p HeadscaleNode) bool { return p.ID == id }) {
		return nil
	}
	return err
}

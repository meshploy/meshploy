package service

import (
	"context"
	"log"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/meshploy/packages/db"
	"github.com/meshploy/packages/hostagent"
	appk8s "github.com/meshploy/packages/server/k8s"
)

// k3sTokenPath is the k3s server's own token, on a gateway where the API runs
// beside the server.
const k3sTokenPath = "/var/lib/rancher/k3s/server/node-token"

// k3sGatewayServerURL is the cluster's API on a gateway, as a node on the mesh
// reaches it.
const k3sGatewayServerURL = "https://100.64.0.1:6443"

// ClusterJoin is what a machine joining the cluster is handed.
type ClusterJoin struct {
	Token     string
	ServerURL string
	// ExpiresAt is when Token stops being accepted; nil for the server's own
	// token, which does not expire.
	ExpiresAt *time.Time
}

// ClusterJoin is the token node joins the cluster with and the server it
// joins. On a gateway that is the server's own token, read at request time so
// a rotated one is current. Through a cluster agent the server's own token is
// never read: the agent mints one for this join that lasts an hour, and the
// server is the node holding the agent.
func (s *NodeService) ClusterJoin(ctx context.Context, node string) (ClusterJoin, error) {
	if s.agent == nil {
		token := s.k3sToken
		if raw, err := os.ReadFile(k3sTokenPath); err == nil {
			token = strings.TrimSpace(string(raw))
		}
		return ClusterJoin{Token: token, ServerURL: k3sGatewayServerURL}, nil
	}
	var edge []db.Node
	if err := s.db.WithContext(ctx).Select("tailscale_ip").
		Where("cluster_agent_cert <> ''").Limit(1).Find(&edge).Error; err != nil {
		return ClusterJoin{}, err
	}
	if len(edge) == 0 {
		return ClusterJoin{}, appk8s.ErrNoClusterAgent
	}
	t, err := s.agent.JoinToken(ctx, joinTokenName(node))
	if err != nil {
		return ClusterJoin{}, err
	}
	return ClusterJoin{Token: t.Token, ServerURL: "https://" + edge[0].TailscaleIP + ":6443", ExpiresAt: &t.ExpiresAt}, nil
}

var notNodeName = regexp.MustCompile(`[^a-z0-9-]+`)

// joinTokenName is node as the agent takes it in a token's description: a
// lowercase label.
func joinTokenName(node string) string {
	n := strings.Trim(notNodeName.ReplaceAllString(strings.ToLower(node), "-"), "-")
	if len(n) > 63 {
		n = strings.Trim(n[:63], "-")
	}
	if n == "" {
		return "node"
	}
	return n
}

// edgeFactsEvery is how often an agent API reads what the edge's install
// recorded.
const edgeFactsEvery = 5 * time.Minute

// SyncEdgeFacts copies what the edge's host agent reports of its install onto
// the node holding the cluster agent: its public address, which the edge is
// published with, and whether published ports are bound to the mesh alone.
// True when the public address changed, so the edge must be published again.
// Nothing to do without an agent or before the host agent has reported.
func (s *NodeService) SyncEdgeFacts(ctx context.Context) (bool, error) {
	if s.agent == nil || s.host == nil {
		return false, nil
	}
	facts, err := hostagent.ReadFacts(s.host)
	if err != nil || facts == nil {
		return false, err
	}
	if s.nodePorts != nil {
		s.nodePorts(facts.NodePortAddresses != "")
	}
	if facts.PublicIP == "" {
		return false, nil
	}
	res := s.db.WithContext(ctx).Model(&db.Node{}).
		Where("cluster_agent_cert <> '' AND public_ip <> ?", facts.PublicIP).
		Update("public_ip", facts.PublicIP)
	return res.RowsAffected > 0, res.Error
}

// FollowEdgeFacts reads the edge's facts every few minutes until ctx ends,
// publishing the edge again when its address moved.
func (s *NodeService) FollowEdgeFacts(ctx context.Context, republish func(context.Context) error) {
	t := time.NewTicker(edgeFactsEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.syncEdgeFactsAndPublish(ctx, republish)
		}
	}
}

func (s *NodeService) syncEdgeFactsAndPublish(ctx context.Context, republish func(context.Context) error) {
	moved, err := s.SyncEdgeFacts(ctx)
	if err != nil {
		log.Printf("warning: the edge's facts: %v", err)
		return
	}
	if moved {
		if err := republish(ctx); err != nil {
			log.Printf("warning: could not hand the edge its new address: %v", err)
		}
	}
}

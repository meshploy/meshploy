package service

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
	"gorm.io/gorm"
)

// SetPrimary moves the primary pointer to another base domain.
//
// It moves a pointer, not the platform. The domain that was primary keeps
// serving its console, api and headscale names as a former primary: somebody
// is very likely using that console as they click this, and every worker
// joined the mesh through that headscale name. Nothing stops answering; the
// new names start answering as well, and the old ones go only when their
// domain is retired and removed.
func (s *DomainService) SetPrimary(ctx context.Context, domainID uuid.UUID) (*db.Domain, error) {
	domain, err := s.Get(ctx, domainID)
	if err != nil {
		return nil, err
	}
	if domain.IsPrimary {
		return domain, nil
	}
	if !domain.Verified {
		return nil, huma.Error422UnprocessableEntity(
			"verify this domain first - nothing is served on it until it is, so the console would not be reachable there")
	}
	if domain.RetiringAt != nil {
		return nil, huma.Error422UnprocessableEntity(
			"this domain is being retired - stop retiring it before making it primary")
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Off first: one primary per organisation is a unique index, and the
		// new one cannot be set while the old one still holds it.
		if err := tx.Model(&db.Domain{}).
			Where("organization_id = ? AND is_primary", domain.OrganizationID).
			Updates(map[string]any{"is_primary": false, "former_primary": true}).Error; err != nil {
			return err
		}
		return tx.Model(domain).Updates(map[string]any{"is_primary": true, "former_primary": false}).Error
	})
	if err != nil {
		return nil, err
	}
	domain.IsPrimary, domain.FormerPrimary = true, false
	return domain, nil
}

// ControlNode is a machine whose Tailscale client reaches Headscale through a
// given domain's headscale name.
type ControlNode struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	TailscaleIP string    `json:"tailscale_ip"`
	Status      string    `json:"status"`
	ControlURL  string    `json:"control_url"`
}

// NodesOnDomain lists the nodes whose control connection goes through this
// domain's headscale name.
//
// That is a blocker for removing it, and only for a domain that is or was
// primary: nothing ever joins through any other. Nodes that joined before the
// URL was recorded joined through the install domain.
func (s *DomainService) NodesOnDomain(ctx context.Context, orgID uuid.UUID, domain *db.Domain) ([]ControlNode, error) {
	out := make([]ControlNode, 0)
	if !domain.IsPrimary && !domain.FormerPrimary {
		return out, nil
	}
	var nodes []db.Node
	if err := s.db.WithContext(ctx).
		Where("organization_id = ? AND k3s_role <> ?", orgID, db.K3sRoleServer).
		Order("name ASC").Find(&nodes).Error; err != nil {
		return nil, err
	}
	want := "headscale." + domain.BaseDomain
	for _, n := range nodes {
		host := controlHost(n.ControlURL)
		if host == "" && s.installDomain != "" {
			host = "headscale." + s.installDomain
		}
		if host == want {
			out = append(out, ControlNode{ID: n.ID, Name: n.Name, TailscaleIP: n.TailscaleIP,
				Status: string(n.Status), ControlURL: "https://" + host})
		}
	}
	return out, nil
}

func controlHost(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

// MarkNodeMoved records that a node now reaches Headscale through the primary's
// name.
//
// It records what the operator did on the machine; it does not do it, and
// cannot check it. The API has no way into a worker, and moving a Tailscale
// client to another control URL means re-authenticating it there - a step to
// run deliberately, on one node first.
func (s *DomainService) MarkNodeMoved(ctx context.Context, orgID, nodeID uuid.UUID) (*db.Node, error) {
	var node db.Node
	if err := s.db.WithContext(ctx).First(&node, "id = ? AND organization_id = ?", nodeID, orgID).Error; err != nil {
		return nil, huma.Error404NotFound("node not found")
	}
	if node.K3sRole == db.K3sRoleServer {
		return nil, huma.Error422UnprocessableEntity(
			"the gateway reaches Headscale over loopback, not through a domain - there is nothing to move")
	}
	u := s.PlatformURL(ctx, orgID, "headscale")
	if u == "" {
		return nil, huma.Error422UnprocessableEntity("this gateway has no primary domain to move the node to")
	}
	node.ControlURL = u
	return &node, s.db.WithContext(ctx).Model(&node).Update("control_url", u).Error
}

// errNodesOnDomain explains why a domain nodes still join through cannot go.
func errNodesOnDomain(n int, domain string) error {
	return huma.Error422UnprocessableEntity(fmt.Sprintf(
		"%d node(s) still reach the mesh through headscale.%s - move them to the primary's headscale first, or they drop off the mesh when this domain goes",
		n, domain))
}

// ServesPlatform reports whether the platform's own names are served on this
// base domain - it is the primary, or a former primary not yet removed.
func (s *DomainService) ServesPlatform(ctx context.Context, baseDomain string) bool {
	var n int64
	s.db.WithContext(ctx).Model(&db.Domain{}).
		Where("base_domain = ? AND (is_primary OR former_primary)", strings.ToLower(baseDomain)).
		Count(&n)
	return n > 0
}

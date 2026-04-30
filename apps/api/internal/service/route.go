package service

import (
	"context"
	"fmt"
	"net"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
	"gorm.io/gorm"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type RouteService struct {
	db  *gorm.DB
	k8s kubernetes.Interface // nil when K8s is not configured
}

type CreateRouteInput struct {
	OrgID     uuid.UUID
	ProjectID uuid.UUID
	ServiceID *uuid.UUID // service target — backend resolves IP:port
	NodeID    *uuid.UUID // node target — use with Port
	Port      int        // port for node target

	// Domain-based fields (preferred path)
	DomainID *uuid.UUID
	Zone     db.RouteZone // public | internal | preview
	Subdomain string

	// Legacy / manual fallback: supply a raw hostname directly
	// (used when DomainID is nil)
	Hostname string

	TargetIP   string
	TargetPort int
}

func (s *RouteService) ListByProject(ctx context.Context, projectID uuid.UUID) ([]db.Route, error) {
	routes := make([]db.Route, 0)
	err := s.db.WithContext(ctx).Where("project_id = ?", projectID).Find(&routes).Error
	return routes, err
}

func (s *RouteService) ListByOrg(ctx context.Context, orgID uuid.UUID) ([]db.Route, error) {
	routes := make([]db.Route, 0)
	err := s.db.WithContext(ctx).Where("organization_id = ?", orgID).Find(&routes).Error
	return routes, err
}

func (s *RouteService) Get(ctx context.Context, routeID uuid.UUID) (*db.Route, error) {
	var route db.Route
	err := s.db.WithContext(ctx).First(&route, "id = ?", routeID).Error
	return &route, err
}

func (s *RouteService) Create(ctx context.Context, in CreateRouteInput) (*db.Route, error) {
	// Resolve target from service or node.
	if in.ServiceID != nil {
		var svc db.Service
		if err := s.db.WithContext(ctx).Preload("Node").Preload("Project").First(&svc, "id = ?", *in.ServiceID).Error; err != nil {
			return nil, huma.Error404NotFound("service not found")
		}
		if svc.NodeID != nil && svc.Node != nil {
			// Service is pinned to a specific node — use it directly.
			in.TargetIP = svc.Node.TailscaleIP
		} else if s.k8s != nil {
			// Auto-scheduled: find the actual node the pod is running on via K8s.
			podSlug := slugify(svc.Name)
			namespace := svc.Project.Slug
			selector := fmt.Sprintf("app=%s,managed-by=meshploy", podSlug)
			pods, err := s.k8s.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
			if err != nil || len(pods.Items) == 0 {
				return nil, huma.Error422UnprocessableEntity("service has no running pods — deploy it first")
			}
			k8sNodeName := pods.Items[0].Spec.NodeName

			// Resolve K8s node → mesh IP via InternalIP addresses, then fall back to name match.
			k8sNode, err := s.k8s.CoreV1().Nodes().Get(ctx, k8sNodeName, metav1.GetOptions{})
			if err != nil {
				return nil, huma.Error422UnprocessableEntity("could not resolve mesh node for service — ensure the worker is registered")
			}
			var node db.Node
			found := false
			for _, addr := range k8sNode.Status.Addresses {
				if addr.Type == corev1.NodeInternalIP {
					if err := s.db.WithContext(ctx).Where("tailscale_ip = ?", addr.Address).First(&node).Error; err == nil {
						found = true
						break
					}
				}
			}
			if !found {
				// Fall back to matching by node name.
				if err := s.db.WithContext(ctx).Where("name = ?", k8sNodeName).First(&node).Error; err != nil {
					return nil, huma.Error422UnprocessableEntity("could not resolve mesh node for service — ensure the worker is registered")
				}
			}
			in.TargetIP = node.TailscaleIP
		} else {
			return nil, huma.Error422UnprocessableEntity("service must be pinned to a specific node to create a route (K8s not configured)")
		}
		targetPort := svc.Port
		if targetPort == 0 {
			targetPort = 3000
		}
		in.TargetPort = targetPort
	} else if in.NodeID != nil {
		var node db.Node
		if err := s.db.WithContext(ctx).First(&node, "id = ?", *in.NodeID).Error; err != nil {
			return nil, huma.Error404NotFound("node not found")
		}
		in.TargetIP = node.TailscaleIP
		in.TargetPort = in.Port
	}

	hostname := in.Hostname

	if in.DomainID != nil {
		var domain db.Domain
		if err := s.db.WithContext(ctx).First(&domain, "id = ?", *in.DomainID).Error; err != nil {
			return nil, huma.Error404NotFound("domain not found")
		}

		// Ownership must be verified before routes can be created.
		if !domain.Verified {
			return nil, huma.Error422UnprocessableEntity("domain ownership not yet verified")
		}

		// Block reserved subdomains from being used as public routes.
		if in.Zone == db.RouteZonePublic {
			if in.Subdomain == domain.InternalSubdomain || in.Subdomain == domain.PreviewSubdomain {
				return nil, huma.Error422UnprocessableEntity(
					fmt.Sprintf("subdomain %q is reserved for %s routing",
						in.Subdomain, in.Subdomain))
			}
		}

		// Derive the full hostname from zone + subdomain.
		switch in.Zone {
		case db.RouteZoneInternal:
			hostname = fmt.Sprintf("%s.%s.%s", in.Subdomain, domain.InternalSubdomain, domain.BaseDomain)
		case db.RouteZonePreview:
			hostname = fmt.Sprintf("%s.%s.%s", in.Subdomain, domain.PreviewSubdomain, domain.BaseDomain)
		default: // public
			hostname = fmt.Sprintf("%s.%s", in.Subdomain, domain.BaseDomain)
		}
	}

	route := &db.Route{
		OrganizationID: in.OrgID,
		ProjectID:      in.ProjectID,
		ServiceID:      in.ServiceID,
		DomainID:       in.DomainID,
		Zone:           in.Zone,
		Subdomain:      in.Subdomain,
		Hostname:       hostname,
		TargetIP:       in.TargetIP,
		TargetPort:     in.TargetPort,
	}

	// Auto-generate a verify token for custom-domain routes so the user can
	// prove DNS ownership before Caddy will issue an On-Demand TLS cert.
	if in.DomainID == nil && hostname != "" {
		token, err := generateVerifyToken()
		if err != nil {
			return nil, fmt.Errorf("generate custom domain verify token: %w", err)
		}
		route.CustomDomainVerifyToken = token
	}

	return route, s.db.WithContext(ctx).Create(route).Error
}

type UpdateRouteInput struct {
	// ServiceID: non-nil = link to service and auto-resolve target IP/port.
	// nil = unlink (use TargetIP/TargetPort directly).
	ServiceID  *uuid.UUID
	UpdateServiceID bool // when true, ServiceID field is applied (allows explicit unlink)
	TargetIP   string
	TargetPort int
}

func (s *RouteService) Update(ctx context.Context, routeID uuid.UUID, in UpdateRouteInput) (*db.Route, error) {
	var route db.Route
	if err := s.db.WithContext(ctx).First(&route, "id = ?", routeID).Error; err != nil {
		return nil, err
	}

	updates := map[string]any{
		"target_ip":   in.TargetIP,
		"target_port": in.TargetPort,
	}

	if in.UpdateServiceID {
		updates["service_id"] = in.ServiceID
		if in.ServiceID != nil {
			var svc db.Service
			if err := s.db.WithContext(ctx).Preload("Node").First(&svc, "id = ?", *in.ServiceID).Error; err != nil {
				return nil, huma.Error400BadRequest("service not found")
			}
			if svc.Node != nil {
				updates["target_ip"] = svc.Node.TailscaleIP
			}
			updates["target_port"] = svc.Port
		}
	}

	err := s.db.WithContext(ctx).Model(&route).Updates(updates).Error
	return &route, err
}

// SyncRouteIP re-resolves the target IP from the service's current node.
// Used when a pod has been rescheduled and the stored IP is stale.
func (s *RouteService) SyncRouteIP(ctx context.Context, routeID uuid.UUID) (*db.Route, error) {
	var route db.Route
	if err := s.db.WithContext(ctx).First(&route, "id = ?", routeID).Error; err != nil {
		return nil, err
	}
	if route.ServiceID == nil {
		return nil, huma.Error400BadRequest("route is not linked to a service")
	}
	var svc db.Service
	if err := s.db.WithContext(ctx).Preload("Node").First(&svc, "id = ?", *route.ServiceID).Error; err != nil {
		return nil, huma.Error400BadRequest("linked service not found")
	}
	var targetIP string
	if svc.Node != nil {
		targetIP = svc.Node.TailscaleIP
	} else {
		// Auto-scheduled: find the actual node by querying the running pod.
		if s.k8s == nil {
			return nil, huma.Error422UnprocessableEntity("kubernetes not configured — cannot resolve auto-scheduled node")
		}
		var project db.Project
		if err := s.db.WithContext(ctx).First(&project, "id = ?", svc.ProjectID).Error; err != nil {
			return nil, huma.Error422UnprocessableEntity("project not found")
		}
		pods, err := s.k8s.CoreV1().Pods(project.Slug).List(ctx, metav1.ListOptions{
			LabelSelector: fmt.Sprintf("app=%s,managed-by=meshploy", slugify(svc.Name)),
		})
		if err != nil || len(pods.Items) == 0 {
			return nil, huma.Error422UnprocessableEntity("no running pod found — deploy the service first")
		}
		nodeName := pods.Items[0].Spec.NodeName
		var node db.Node
		if err := s.db.WithContext(ctx).First(&node, "name = ?", nodeName).Error; err != nil {
			return nil, huma.Error422UnprocessableEntity("node '" + nodeName + "' not found in Meshploy — is it registered?")
		}
		targetIP = node.TailscaleIP
	}
	err := s.db.WithContext(ctx).Model(&route).Updates(map[string]any{
		"target_ip":   targetIP,
		"target_port": svc.Port,
	}).Error
	return &route, err
}

func (s *RouteService) Delete(ctx context.Context, routeID uuid.UUID) error {
	return s.db.WithContext(ctx).Delete(&db.Route{}, "id = ?", routeID).Error
}

// VerifyCustomHostname checks the DNS TXT record for a custom-domain route and,
// on success, marks it verified so Caddy's domain-check endpoint returns 200.
// TXT record expected: _meshploy-verify.{hostname} = {custom_domain_verify_token}
func (s *RouteService) VerifyCustomHostname(ctx context.Context, routeID uuid.UUID) (*db.Route, error) {
	route, err := s.Get(ctx, routeID)
	if err != nil {
		return nil, huma.Error404NotFound("route not found")
	}
	if route.DomainID != nil {
		return nil, huma.Error400BadRequest("route uses a managed domain — no custom-domain verification needed")
	}
	if route.CustomDomainVerified {
		return route, nil
	}
	records, err := net.LookupTXT("_meshploy-verify." + route.Hostname)
	if err != nil || !containsToken(records, route.CustomDomainVerifyToken) {
		return nil, huma.Error422UnprocessableEntity("TXT record not found or not yet propagated")
	}
	route.CustomDomainVerified = true
	err = s.db.WithContext(ctx).Model(route).Update("custom_domain_verified", true).Error
	return route, err
}

// IsCustomDomainVerified is the fast lookup used by the domain-check endpoint.
// Returns true only for routes with a custom (unmanaged) hostname that has been
// explicitly verified via DNS TXT record.
func (s *RouteService) IsCustomDomainVerified(ctx context.Context, hostname string) bool {
	var route db.Route
	err := s.db.WithContext(ctx).
		Where("hostname = ? AND domain_id IS NULL AND custom_domain_verified = ?", hostname, true).
		First(&route).Error
	return err == nil
}

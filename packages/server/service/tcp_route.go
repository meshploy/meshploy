package service

import (
	"context"
	"fmt"
	"net"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
	"github.com/meshploy/packages/hostagent"
	appk8s "github.com/meshploy/packages/server/k8s"
	"gorm.io/gorm"
	"k8s.io/client-go/kubernetes"
)

type TCPRouteService struct {
	db  *gorm.DB
	k8s kubernetes.Interface
	// hostDir is where the host agent reports; empty on an API with no config.
	hostDir string
}

// withHostFirewall reads the host agent's last report once and says, for each
// route, what the host firewall does with its port.
func (s *TCPRouteService) withHostFirewall(routes []db.TCPRoute) {
	if s.hostDir == "" || len(routes) == 0 {
		return
	}
	agent, fw, err := hostagent.ReadState(s.hostDir)
	now := time.Now()
	for i := range routes {
		var v hostagent.PortVerdict
		if err != nil {
			v = hostagent.PortVerdict{State: hostagent.PortUnknown, Reason: "the host agent's report could not be read"}
		} else {
			v = hostagent.Evaluate(agent, fw, routes[i].GatewayPort, "tcp", now)
		}
		routes[i].HostFirewall = &db.PortFirewall{State: v.State, Tool: v.Tool, Sources: v.Sources, CheckedAt: v.CheckedAt, Reason: v.Reason}
	}
}

// gatewayOwnPorts are the gateway's own listeners. Routing one would take the
// console, the mesh or the cluster off the air, and the failure would look like
// the machine being broken rather than like a route.
//
// Kept beside the exposure notice's list, which describes the same machine from
// the other direction: what it publishes today.
var gatewayOwnPorts = []int{22, 53, 80, 443, 2019, 4000, 5000, 6443, 8081, 8085, 9090, 9100, 10250}

// ── Input types ───────────────────────────────────────────────────────────────

type CreateTCPRouteInput struct {
	OrgID       uuid.UUID
	ProjectID   uuid.UUID
	GatewayPort int

	// The target: a service's published port, or a port on a node.
	ServiceID   *uuid.UUID
	ServicePort int // container port; 0 = the service's own published port
	NodeID      *uuid.UUID
	NodePort    int // the port on that node

	AllowedCIDRs []string

	// Paused creates the route without opening its port. The zero value
	// publishes.
	Paused bool
}

// UpdateTCPRouteInput changes only what it carries: a nil field keeps what the
// route has. Retargeting is a delete and a create, so only the port and the
// allow-list can change.
type UpdateTCPRouteInput struct {
	GatewayPort  *int
	AllowedCIDRs []string
}

// ── CRUD ──────────────────────────────────────────────────────────────────────

func (s *TCPRouteService) List(ctx context.Context, projectID uuid.UUID) ([]db.TCPRoute, error) {
	var routes []db.TCPRoute
	err := s.db.WithContext(ctx).
		Preload("Service").Preload("Node").
		Where("project_id = ?", projectID).
		Order("gateway_port ASC").
		Find(&routes).Error
	s.withHostFirewall(routes)
	return routes, err
}

// ListForOrg returns every published port in the organization. A gateway port
// is unique across the gateway, not per project, so a form that only knew its
// own project's ports would offer one that is already taken.
func (s *TCPRouteService) ListForOrg(ctx context.Context, orgID uuid.UUID) ([]db.TCPRoute, error) {
	var routes []db.TCPRoute
	err := s.db.WithContext(ctx).
		Preload("Service").Preload("Node").
		Where("organization_id = ?", orgID).
		Order("gateway_port ASC").
		Find(&routes).Error
	s.withHostFirewall(routes)
	return routes, err
}

// ReservedPorts are the gateway's own, which a route may never take.
func ReservedPorts() []int { return gatewayOwnPorts }

func (s *TCPRouteService) Get(ctx context.Context, routeID, projectID uuid.UUID) (*db.TCPRoute, error) {
	var route db.TCPRoute
	err := s.db.WithContext(ctx).
		Preload("Service").Preload("Node").
		First(&route, "id = ? AND project_id = ?", routeID, projectID).Error
	if err == nil {
		one := []db.TCPRoute{route}
		s.withHostFirewall(one)
		route = one[0]
	}
	return &route, err
}

func (s *TCPRouteService) Create(ctx context.Context, in CreateTCPRouteInput) (*db.TCPRoute, error) {
	if err := s.checkPort(ctx, in.GatewayPort, uuid.Nil); err != nil {
		return nil, err
	}
	cidrs, err := parseCIDRs(in.AllowedCIDRs)
	if err != nil {
		return nil, err
	}

	route := &db.TCPRoute{
		OrganizationID: in.OrgID,
		ProjectID:      in.ProjectID,
		GatewayPort:    in.GatewayPort,
		ServiceID:      in.ServiceID,
		NodeID:         in.NodeID,
		AllowedCIDRs:   cidrs,
		Status:         db.TCPRoutePending,
	}
	if err := s.resolveTarget(ctx, route, in); err != nil {
		return nil, err
	}
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(route).Error; err != nil {
			return err
		}
		// A false bool with a column default is left out of the insert, so a
		// paused route is written published and paused in the same transaction.
		if in.Paused {
			return tx.Model(route).Updates(map[string]any{"published": false, "status": db.TCPRoutePaused}).Error
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return s.Get(ctx, route.ID, in.ProjectID)
}

func (s *TCPRouteService) Update(ctx context.Context, routeID, projectID uuid.UUID, in UpdateTCPRouteInput) (*db.TCPRoute, error) {
	route, err := s.Get(ctx, routeID, projectID)
	if err != nil {
		return nil, err
	}
	updates := map[string]any{}
	if in.GatewayPort != nil && *in.GatewayPort != route.GatewayPort {
		if err := s.checkPort(ctx, *in.GatewayPort, routeID); err != nil {
			return nil, err
		}
		updates["gateway_port"] = *in.GatewayPort
		// The gateway has to move the listener, so the route is pending again
		// until it reports back.
		updates["status"] = db.TCPRoutePending
		updates["last_error"] = ""
	}
	if in.AllowedCIDRs != nil {
		cidrs, err := parseCIDRs(in.AllowedCIDRs)
		if err != nil {
			return nil, err
		}
		updates["allowed_cidrs"] = cidrs
	}
	if len(updates) > 0 {
		if err := s.db.WithContext(ctx).Model(route).Updates(updates).Error; err != nil {
			return nil, err
		}
	}
	return s.Get(ctx, routeID, projectID)
}

// SetPublished opens or closes a route's port without deleting the route, and
// records who did it. The route is pending until the gateway reports back.
func (s *TCPRouteService) SetPublished(ctx context.Context, routeID, projectID uuid.UUID, published bool, by uuid.UUID) (*db.TCPRoute, error) {
	route, err := s.Get(ctx, routeID, projectID)
	if err != nil {
		return nil, err
	}
	if err := s.db.WithContext(ctx).Model(route).Updates(map[string]any{
		"published":            published,
		"published_changed_at": time.Now(),
		"published_changed_by": by,
		"status":               db.TCPRoutePending,
		"last_error":           "",
	}).Error; err != nil {
		return nil, err
	}
	return s.Get(ctx, routeID, projectID)
}

func (s *TCPRouteService) Delete(ctx context.Context, routeID, projectID uuid.UUID) error {
	return s.db.WithContext(ctx).
		Where("id = ? AND project_id = ?", routeID, projectID).
		Delete(&db.TCPRoute{}).Error
}

// Retarget re-resolves an existing route's address. A deploy assigns a new
// NodePort, and a route pointing at the old one forwards to nothing.
func (s *TCPRouteService) Retarget(ctx context.Context, serviceID uuid.UUID) {
	var routes []db.TCPRoute
	if err := s.db.WithContext(ctx).Where("service_id = ?", serviceID).Find(&routes).Error; err != nil {
		return
	}
	for i := range routes {
		r := &routes[i]
		in := CreateTCPRouteInput{ServiceID: r.ServiceID, ServicePort: r.ServicePort}
		fresh := *r
		if err := s.resolveTarget(ctx, &fresh, in); err != nil {
			continue // the service has no published port right now; leave the route alone
		}
		if fresh.TargetIP == r.TargetIP && fresh.TargetPort == r.TargetPort {
			continue
		}
		s.db.WithContext(ctx).Model(r).Updates(map[string]any{
			"target_ip":   fresh.TargetIP,
			"target_port": fresh.TargetPort,
			"status":      db.TCPRoutePending,
		})
	}
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// checkPort refuses a port the gateway cannot give away: outside the range, one
// of its own, or already routed.
func (s *TCPRouteService) checkPort(ctx context.Context, port int, exceptID uuid.UUID) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("a gateway port must be between 1 and 65535")
	}
	if slices.Contains(gatewayOwnPorts, port) {
		return fmt.Errorf("port %d is the gateway's own; pick another", port)
	}
	q := s.db.WithContext(ctx).Model(&db.TCPRoute{}).Where("gateway_port = ?", port)
	if exceptID != uuid.Nil {
		q = q.Where("id <> ?", exceptID)
	}
	var count int64
	if err := q.Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return fmt.Errorf("port %d is already routed", port)
	}
	return nil
}

// resolveTarget writes the address the gateway forwards to, the way an HTTP
// route target is resolved: through the service's NodePort on the gateway node,
// so kube-proxy spreads connections across replicas wherever they run.
func (s *TCPRouteService) resolveTarget(ctx context.Context, route *db.TCPRoute, in CreateTCPRouteInput) error {
	switch {
	case in.NodeID != nil:
		var node db.Node
		if err := s.db.WithContext(ctx).First(&node, "id = ?", *in.NodeID).Error; err != nil {
			return fmt.Errorf("node not found")
		}
		if in.NodePort < 1 || in.NodePort > 65535 {
			return fmt.Errorf("a target port must be between 1 and 65535")
		}
		route.TargetIP = node.TailscaleIP
		route.TargetPort = in.NodePort
		return nil

	case in.ServiceID != nil:
		var svc db.Service
		if err := s.db.WithContext(ctx).Preload("Project").Preload("Ports").
			First(&svc, "id = ?", *in.ServiceID).Error; err != nil {
			return fmt.Errorf("service not found")
		}
		port, nodePort, err := s.servicePort(ctx, &svc, in.ServicePort)
		if err != nil {
			return err
		}
		gateway, err := s.gatewayNode(ctx)
		if err != nil {
			return err
		}
		route.ServicePort = port
		route.TargetIP = gateway.TailscaleIP
		route.TargetPort = nodePort
		return nil
	}
	return fmt.Errorf("a route needs a target: a service or a node")
}

// servicePort picks which port of a service to route, and returns it with the
// NodePort it is published on.
//
// A managed database is published by its own network access setting rather than
// by a port row, so it is read from there: a TCP route to a database is that
// setting plus a way in from the gateway, and asking for one without the other
// would forward to a port the cluster never opened.
func (s *TCPRouteService) servicePort(ctx context.Context, svc *db.Service, want int) (int, int, error) {
	if svc.Type == db.ServiceTypeDatabase {
		var dc db.DatabaseConfig
		if err := s.db.WithContext(ctx).Where("service_id = ?", svc.ID).First(&dc).Error; err != nil {
			return 0, 0, fmt.Errorf("database config not found")
		}
		if !dc.MeshExposed || dc.NodePort == 0 {
			return 0, 0, fmt.Errorf("%s is in-cluster only: give it mesh access first, then route it", svc.Name)
		}
		port := want
		if port == 0 {
			port = int(primaryPort(svc.Ports))
		}
		return port, dc.NodePort, nil
	}

	var match *db.ServicePort
	for i := range svc.Ports {
		p := &svc.Ports[i]
		if !p.IsPublic || p.IsHTTP {
			continue // an HTTP port takes a domain route, which gives it TLS
		}
		if want != 0 && p.Port != want {
			continue
		}
		if match == nil || p.IsPrimary {
			match = p
		}
	}
	if match == nil {
		return 0, 0, fmt.Errorf("%s has no routable non-HTTP port: an HTTP port is published with a domain route instead", svc.Name)
	}
	// The row mirrors what the cluster assigned, so a zero may be drift rather
	// than an undeployed service. Ask the cluster, and write the answer back.
	if match.NodePort == 0 && s.k8s != nil && svc.Project.Slug != "" {
		if np, err := appk8s.GetNodePort(ctx, s.k8s, appK8sName(svc), svc.Project.Slug, int32(match.Port)); err == nil && np != 0 {
			match.NodePort = int(np)
			s.db.WithContext(ctx).Model(&db.ServicePort{}).Where("id = ?", match.ID).Update("node_port", np)
		}
	}
	if match.NodePort == 0 {
		return 0, 0, fmt.Errorf("no port is published for %s yet: deploy it, then add the route", svc.Name)
	}
	return match.Port, match.NodePort, nil
}

func (s *TCPRouteService) gatewayNode(ctx context.Context) (*db.Node, error) {
	var gateway db.Node
	if err := s.db.WithContext(ctx).
		Where("k3s_role = ? AND status = ?", db.K3sRoleServer, "online").
		First(&gateway).Error; err != nil {
		return nil, fmt.Errorf("the gateway node is not online, so a route cannot be resolved")
	}
	return &gateway, nil
}

// parseCIDRs accepts what an operator would type: a bare address as well as a
// range, since "only from my laptop" is the common case.
func parseCIDRs(in []string) (db.StringArray, error) {
	out := make(db.StringArray, 0, len(in))
	for _, raw := range in {
		v := strings.TrimSpace(raw)
		if v == "" {
			continue
		}
		if !strings.Contains(v, "/") {
			ip := net.ParseIP(v)
			if ip == nil {
				return nil, fmt.Errorf("%q is not an address or a range", raw)
			}
			if ip.To4() != nil {
				v += "/32"
			} else {
				v += "/128"
			}
		}
		if _, _, err := net.ParseCIDR(v); err != nil {
			return nil, fmt.Errorf("%q is not an address or a range", raw)
		}
		out = append(out, v)
	}
	return out, nil
}

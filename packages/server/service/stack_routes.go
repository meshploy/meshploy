package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"

	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/google/uuid"
	"gopkg.in/yaml.v3"
	"gorm.io/gorm"

	meshdb "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/templates"
)

// meshployRoute is one address a stack service is served at, declared in its
// x-meshploy block:
//
//	x-meshploy:
//	  route:
//	    subdomain: app       # required
//	    domain: example.com  # a verified base domain; default: the project's own, else the primary
//	    port: 3000           # default: the service's primary port
//	    zone: internal       # public (default) or internal
//	    published: false     # made paused; default: served once the service runs
//
// `route:` is one mapping or a list of them.
type meshployRoute struct {
	Subdomain string `json:"subdomain"`
	Domain    string `json:"domain"`
	Port      int    `json:"port"`
	Zone      string `json:"zone"`
	Published *bool  `json:"published"`
}

// meshployRoutes reads `route:` as one mapping or a list.
type meshployRoutes []meshployRoute

func (r *meshployRoutes) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || bytes.Equal(b, []byte("null")) {
		*r = nil
		return nil
	}
	if b[0] == '[' {
		var list []meshployRoute
		if err := json.Unmarshal(b, &list); err != nil {
			return err
		}
		*r = list
		return nil
	}
	var one meshployRoute
	if err := json.Unmarshal(b, &one); err != nil {
		return err
	}
	*r = meshployRoutes{one}
	return nil
}

// declaredRoute is a route the file asks for, resolved to what it names.
type declaredRoute struct {
	service *meshdb.Service
	decl    meshployRoute
	domain  *meshdb.Domain
	zone    meshdb.RouteZone
}

// declaredRouteKey is what makes two routes the same one: a declaration and a route row
// match on domain, zone and subdomain, which is the hostname before a level
// derives its own.
func declaredRouteKey(domainID uuid.UUID, zone meshdb.RouteZone, subdomain string) string {
	return domainID.String() + "|" + string(zone) + "|" + subdomain
}

// reconcileStackRoutes makes the routes the stack's file declares and removes
// the ones it made that the file no longer names.
//
// A route is made with its service as the target. A service that has not run
// yet has no address, so its route waits, paused, and its first successful
// deploy publishes it (RouteService.PublishAwaitingRoutes): the route exists the
// moment the apply returns and cannot be lost to a wait that times out.
//
// Only routes the file declared are ever changed or removed (StackDeclared). A
// route with the declared hostname that the stack made some other way (a
// template deploy before routes were declared) or that was made by hand for
// one of the stack's services is taken over; any other route holding that
// hostname is a conflict, reported for that route alone.
func (s *StackService) reconcileStackRoutes(ctx context.Context, stack *meshdb.Stack, project *composetypes.Project, result *ApplyResult) {
	if s.routes == nil {
		return
	}
	var proj meshdb.Project
	if err := s.db.WithContext(ctx).Select("id", "organization_id").First(&proj, "id = ?", stack.ProjectID).Error; err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("routes: %v", err))
		return
	}

	var svcs []meshdb.Service
	s.db.WithContext(ctx).Where("stack_id = ?", stack.ID).Find(&svcs)
	byName := make(map[string]*meshdb.Service, len(svcs))
	for i := range svcs {
		byName[svcs[i].Name] = &svcs[i]
	}

	var existing []meshdb.Route
	s.db.WithContext(ctx).Preload("Targets").Where("project_id = ?", stack.ProjectID).Find(&existing)
	byKey := make(map[string]*meshdb.Route, len(existing))
	for i := range existing {
		r := &existing[i]
		if r.DomainID != nil {
			byKey[declaredRouteKey(*r.DomainID, r.Zone, r.Subdomain)] = r
		}
	}

	names := make([]string, 0, len(project.Services))
	for name := range project.Services {
		names = append(names, name)
	}
	sort.Strings(names)

	kept := map[uuid.UUID]bool{}
	for _, name := range names {
		ext := decodeExt(project.Services[name].Extensions)
		if ext == nil || len(ext.Route) == 0 {
			continue
		}
		svc := byName[name]
		if svc == nil {
			// Its create failed, which the apply has reported already.
			continue
		}
		for _, decl := range ext.Route {
			d, err := s.resolveDeclaredRoute(ctx, proj, svc, decl)
			if err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("%s: route: %v", name, err))
				continue
			}
			port := decl.Port
			if port == 0 {
				if port = s.primaryPort(ctx, svc.ID); port == 0 {
					result.Errors = append(result.Errors, fmt.Sprintf("%s: route %s: the service has no port: name one with port:", name, decl.Subdomain))
					continue
				}
			}
			if r := byKey[declaredRouteKey(d.domain.ID, d.zone, decl.Subdomain)]; r != nil {
				took, err := s.takeOverRoute(ctx, stack, svc, port, r)
				if err != nil {
					result.Errors = append(result.Errors, fmt.Sprintf("%s: route %s: %v", name, r.Hostname, err))
					continue
				}
				if took {
					result.Routes = append(result.Routes, r.Hostname)
				}
				kept[r.ID] = true
				continue
			}
			r, err := s.routes.Create(ctx, CreateRouteInput{
				OrgID:     proj.OrganizationID,
				ProjectID: proj.ID,
				DomainID:  &d.domain.ID,
				Zone:      d.zone,
				Subdomain: decl.Subdomain,
				// The port by number, so a port compose kept internal (expose:)
				// becomes the routable one, as naming it on a route always does.
				Targets: []TargetInput{{ServiceID: &svc.ID, Port: port, AllowUnresolved: true}},
				Paused:  decl.Published != nil && !*decl.Published,
			})
			if err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("%s: route %s: %v", name, decl.Subdomain, err))
				continue
			}
			if err := s.db.WithContext(ctx).Model(r).
				Updates(map[string]any{"stack_id": stack.ID, "stack_declared": true}).Error; err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("%s: route %s: %v", name, r.Hostname, err))
				continue
			}
			kept[r.ID] = true
			byKey[declaredRouteKey(d.domain.ID, d.zone, decl.Subdomain)] = r
			result.Routes = append(result.Routes, r.Hostname)
		}
	}

	// What the file declared before and no longer does.
	for i := range existing {
		r := &existing[i]
		if !r.StackDeclared || r.StackID == nil || *r.StackID != stack.ID || kept[r.ID] {
			continue
		}
		if err := s.routes.Delete(ctx, r.ID); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("route %s: %v", r.Hostname, err))
			continue
		}
		result.RoutesDeleted = append(result.RoutesDeleted, r.Hostname)
	}
}

// primaryPort is the container port a route defaults to: the service's
// primary, else its first. Zero when it has none.
func (s *StackService) primaryPort(ctx context.Context, serviceID uuid.UUID) int {
	var sp meshdb.ServicePort
	if err := s.db.WithContext(ctx).Where("service_id = ?", serviceID).
		Order("is_primary DESC, created_at ASC").First(&sp).Error; err != nil {
		return 0
	}
	return sp.Port
}

// resolveDeclaredRoute checks a declaration and finds the domain it names.
func (s *StackService) resolveDeclaredRoute(ctx context.Context, proj meshdb.Project, svc *meshdb.Service, decl meshployRoute) (*declaredRoute, error) {
	if decl.Subdomain == "" {
		return nil, errors.New("subdomain is required")
	}
	var zone meshdb.RouteZone
	switch decl.Zone {
	case "", "public":
		zone = meshdb.RouteZonePublic
	case "internal":
		zone = meshdb.RouteZoneInternal
	default:
		return nil, fmt.Errorf("zone %q: use public or internal", decl.Zone)
	}
	var d *meshdb.Domain
	if decl.Domain != "" {
		var found meshdb.Domain
		err := s.db.WithContext(ctx).
			Where("organization_id = ? AND base_domain = ?", proj.OrganizationID, decl.Domain).
			First(&found).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%s is not one of this organization's domains", decl.Domain)
		}
		if err != nil {
			return nil, err
		}
		d = &found
	} else {
		found, err := defaultRouteDomain(ctx, s.db, proj.ID)
		if err != nil {
			return nil, err
		}
		if found == nil {
			return nil, errors.New("no verified domain to put it on: add one, or name it with domain:")
		}
		d = found
	}
	return &declaredRoute{service: svc, decl: decl, domain: d, zone: zone}, nil
}

// takeOverRoute makes an existing route holding a declared hostname the
// stack's declared one, when it may, and reports whether it was not already.
//
// One the file declared before stays, retargeted when the file now gives the
// hostname to another of its services. One the stack made some other way, or
// one made by hand that serves only this service, is taken over. Anything else
// holding the hostname is a conflict.
func (s *StackService) takeOverRoute(ctx context.Context, stack *meshdb.Stack, svc *meshdb.Service, port int, r *meshdb.Route) (bool, error) {
	ours := r.StackID != nil && *r.StackID == stack.ID
	serves := len(r.Targets) > 0
	for _, t := range r.Targets {
		if t.ServiceID == nil || *t.ServiceID != svc.ID {
			serves = false
		}
	}
	if ours && r.StackDeclared {
		if serves {
			return false, nil
		}
		return false, s.retargetRoute(ctx, r, svc, port)
	}
	if !ours && !(r.StackID == nil && serves) {
		return false, errors.New("already routed to something else in this project")
	}
	return true, s.db.WithContext(ctx).Model(&meshdb.Route{}).Where("id = ?", r.ID).
		Updates(map[string]any{"stack_id": stack.ID, "stack_declared": true}).Error
}

// retargetRoute points a declared route at another service of the stack. A
// service that has not run yet has no address, so a live route waits for its
// deploy, as a new one does.
func (s *StackService) retargetRoute(ctx context.Context, r *meshdb.Route, svc *meshdb.Service, port int) error {
	for _, t := range r.Targets {
		if err := s.routes.DeleteTarget(ctx, t.ID); err != nil {
			return err
		}
	}
	t, err := s.routes.AddTarget(ctx, r.ID, TargetInput{ServiceID: &svc.ID, Port: port, AllowUnresolved: true})
	if err != nil {
		return err
	}
	if r.Published && t.TargetPort == 0 {
		return s.db.WithContext(ctx).Model(&meshdb.Route{}).Where("id = ?", r.ID).
			Updates(map[string]any{"published": false, "awaiting_deploy": true}).Error
	}
	return nil
}

// defaultRouteDomain is the base domain a route goes on when none is named: a
// domain reserved for the project first, since it is the project's own
// address, then the primary, never a retiring one. Nil when the organization
// has no verified domain.
func defaultRouteDomain(ctx context.Context, gdb *gorm.DB, projectID uuid.UUID) (*meshdb.Domain, error) {
	var p meshdb.Project
	if err := gdb.WithContext(ctx).Select("organization_id").First(&p, "id = ?", projectID).Error; err != nil {
		return nil, err
	}
	root, err := rootProject(ctx, gdb, projectID)
	if err != nil {
		return nil, err
	}
	var d meshdb.Domain
	err = gdb.WithContext(ctx).
		Where("organization_id = ? AND verified = ? AND retiring_at IS NULL AND (project_id IS NULL OR project_id = ?)", p.OrganizationID, true, root).
		Order("project_id IS NULL, is_primary DESC, created_at ASC").First(&d).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// declareExposedRoutes writes a template's exposed services into its compose
// file as `x-meshploy.route`, so the stack's apply makes their routes. A
// service whose block already declares a route, as an edited template's may,
// keeps it.
//
// Like FillMeshployDefaults it inserts lines rather than re-encoding, so the
// rest of the file stays as written, and it fails rather than return a file
// that differs anywhere but in the routes it added.
func declareExposedRoutes(spec string, exposes []templates.ResolvedExpose) (string, error) {
	if len(exposes) == 0 {
		return spec, nil
	}
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(spec), &doc); err != nil {
		return spec, fmt.Errorf("invalid YAML: %w", err)
	}
	if len(doc.Content) == 0 {
		return spec, errors.New("the compose file is empty")
	}
	services := mappingValue(doc.Content[0], "services")
	if !isBlockMapping(services) {
		return spec, errors.New("the compose file has no services block")
	}

	byService := map[string][]templates.ResolvedExpose{}
	var order []string
	for _, e := range exposes {
		if _, ok := byService[e.Service]; !ok {
			order = append(order, e.Service)
		}
		byService[e.Service] = append(byService[e.Service], e)
	}

	f := &specFiller{lines: strings.Split(spec, "\n"), after: map[int][]string{}}
	if strings.Contains(spec, "\r\n") {
		f.eol = "\r"
	}
	added := map[string]bool{}
	for _, name := range order {
		svc, key := mappingEntry(services, name)
		if key == nil {
			return spec, fmt.Errorf("exposed service %q is not in the compose file", name)
		}
		if !isBlockMapping(svc) || svc.Anchor != "" {
			return spec, fmt.Errorf("service %q: its route cannot be written into it", name)
		}
		indent := svc.Content[0].Column - 1
		step := indent - (key.Column - 1)
		if step <= 0 {
			step = 2
		}
		route := routeLines(f, byService[name], step)
		xm, xmKey := mappingEntry(svc, "x-meshploy")
		switch {
		case xmKey == nil:
			lines := []string{f.line(indent, "x-meshploy:")}
			for _, l := range route {
				lines = append(lines, f.line(indent+step, l))
			}
			f.insert(f.end(key), lines)
		case isEmptyNode(xm):
			var lines []string
			for _, l := range route {
				lines = append(lines, f.line(indent+step, l))
			}
			f.insert(f.end(xmKey), lines)
		case isBlockMapping(xm) && xm.Anchor == "":
			if _, k := mappingEntry(xm, "route"); k != nil {
				continue // declared already
			}
			secIndent := xm.Content[0].Column - 1
			var lines []string
			for _, l := range route {
				lines = append(lines, f.line(secIndent, l))
			}
			f.insert(f.end(xmKey), lines)
		default:
			return spec, fmt.Errorf("service %q: its x-meshploy block cannot be written into", name)
		}
		added[name] = true
	}
	if len(added) == 0 {
		return spec, nil
	}
	out := f.String()
	if err := checkRoutesDeclared(spec, out, added); err != nil {
		return spec, err
	}
	return out, nil
}

// routeLines is a `route:` entry for es, relative to the x-meshploy block's
// own indent: a mapping for one address, a list for several.
func routeLines(f *specFiller, es []templates.ResolvedExpose, step int) []string {
	pad := strings.Repeat(" ", step)
	if len(es) == 1 {
		return []string{
			"route:",
			pad + "subdomain: " + yamlValue(es[0].Subdomain),
			pad + "port: " + yamlValue(es[0].Port),
		}
	}
	lines := []string{"route:"}
	for _, e := range es {
		lines = append(lines,
			pad+"- subdomain: "+yamlValue(e.Subdomain),
			pad+"  port: "+yamlValue(e.Port))
	}
	return lines
}

// checkRoutesDeclared fails unless after is before with a route added to each
// service in added, and nothing else changed.
func checkRoutesDeclared(before, after string, added map[string]bool) error {
	var b, a map[string]any
	if err := yaml.Unmarshal([]byte(before), &b); err != nil {
		return err
	}
	if err := yaml.Unmarshal([]byte(after), &a); err != nil {
		return fmt.Errorf("declaring the routes would break the YAML: %w", err)
	}
	bServices := childMap(b, "services")
	for name := range added {
		svc := childMap(childMap(a, "services"), name)
		xm := childMap(svc, "x-meshploy")
		if _, ok := xm["route"]; !ok {
			return fmt.Errorf("declaring %s's route misplaced it", name)
		}
		delete(xm, "route")
		if len(xm) == 0 {
			bxm, had := childMap(bServices, name)["x-meshploy"]
			switch {
			case !had:
				delete(svc, "x-meshploy")
			case bxm == nil:
				svc["x-meshploy"] = nil
			}
		}
	}
	if !reflect.DeepEqual(b, a) {
		return errors.New("declaring the routes would change more than x-meshploy.route")
	}
	return nil
}

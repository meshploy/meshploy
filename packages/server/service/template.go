package service

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	meshdb "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/config"
	"github.com/meshploy/packages/server/templates"
	"gorm.io/gorm"
)

// TemplateService deploys one-click templates. A template is a third stack
// source: Deploy resolves its variables, substitutes them into the compose
// (templates.PrepareSpec — the only deploy-time spec conversion), then lowers to
// the existing stack create + apply + route machinery.
type TemplateService struct {
	db      *gorm.DB
	cfg     *config.Config
	catalog templates.Catalog
	stacks  *StackService
	routes  *RouteService
}

// refresher is a catalog that can be asked to re-read its source. The remote
// catalog implements it; a local directory and the embedded snapshot do not,
// because there is nothing to re-fetch.
type refresher interface {
	Refresh(ctx context.Context) error
}

// Refresh re-reads the catalog now instead of waiting for the next scheduled
// poll.
//
// The cache exists so a busy page does not hammer GitHub, but it also means a
// template published minutes ago is invisible for up to an hour, with nothing
// to do about it but restart the API. Publishing is exactly when someone wants
// to see the result, so it is worth being able to ask.
func (s *TemplateService) Refresh(ctx context.Context) error {
	r, ok := s.catalog.(refresher)
	if !ok {
		// A local or embedded catalog is already whatever its source says.
		return nil
	}
	return r.Refresh(ctx)
}

// List returns the catalog manifests (empty if no catalog is configured).
func (s *TemplateService) List() ([]*templates.Manifest, error) {
	return s.catalog.List()
}

// Get returns a single template.
func (s *TemplateService) Get(id string) (*templates.Template, error) {
	return s.catalog.Get(id)
}

// Icon returns a template's icon bytes and content type.
func (s *TemplateService) Icon(id string) ([]byte, string, error) {
	return s.catalog.Icon(id)
}

// Deploy instantiates templateID into projectID as a stack.
//
// spec, when non-empty, is the (possibly user-edited) compose from the stack
// editor; otherwise the template's own compose is used. promptValues supply the
// prompted variables. The created stack records template provenance, is applied,
// and a public route is created for each exposed service.
func (s *TemplateService) Deploy(ctx context.Context, projectID uuid.UUID, templateID, spec string, promptValues map[string]string, triggeredBy uuid.UUID) (*meshdb.Stack, error) {
	tpl, err := s.catalog.Get(templateID)
	if err != nil {
		return nil, err
	}
	if spec != "" {
		tpl.Compose = spec // honor the user's inline edits
	}

	// Resolve the org's verified base domain (for subdomain assignment + routing).
	domainID, baseDomain := s.orgBaseDomain(ctx, projectID)

	resolvedSpec, vars, exposes, err := tpl.PrepareSpec(promptValues, baseDomain)
	if err != nil {
		return nil, err
	}

	stack, err := s.stacks.Create(ctx, projectID, CreateStackInput{
		Name:            tpl.Manifest.ID,
		Spec:            resolvedSpec,
		Variables:       vars,
		TemplateID:      tpl.Manifest.ID,
		TemplateVersion: tpl.Manifest.Version,
	})
	if err != nil {
		return nil, err
	}

	// Reconcile services/volumes from the resolved spec.
	if _, err := s.stacks.Apply(ctx, stack.ID, triggeredBy, nil); err != nil {
		return stack, fmt.Errorf("template deployed but reconcile failed: %w", err)
	}

	// Create a public route per exposed service, in the background.
	//
	// This cannot be done inline. A route target resolves to the service's
	// NodePort, and that is assigned by the deployment -- which Apply starts in
	// its own goroutine. Inline, the port is always still zero, so route
	// creation returned "service has not been deployed yet" every single time
	// and the error was discarded: a template declaring `expose` produced a
	// deployed app with no way to reach it, and nothing in the logs.
	//
	// Waiting here instead would hold the HTTP request open for the length of a
	// rollout, so the wait is detached and bounded.
	if len(exposes) > 0 {
		go s.createExposedRoutes(context.Background(), tpl.Manifest.ID, stack.ID, projectID, domainID, exposes)
	}

	return stack, nil
}

// orgBaseDomain returns the org's first verified domain (id + base domain) for
// the project, falling back to the configured DOMAIN for the base name when no
// domain record exists. A nil id means no domain-based routing is possible.
func (s *TemplateService) orgBaseDomain(ctx context.Context, projectID uuid.UUID) (*uuid.UUID, string) {
	orgID, err := s.projectOrgID(ctx, projectID)
	if err != nil {
		return nil, s.cfgDomain()
	}
	var d meshdb.Domain
	err = s.db.WithContext(ctx).
		Where("organization_id = ? AND verified = ?", orgID, true).
		Order("created_at ASC").First(&d).Error
	if err != nil {
		return nil, s.cfgDomain()
	}
	id := d.ID
	return &id, d.BaseDomain
}

func (s *TemplateService) cfgDomain() string {
	if s.cfg != nil {
		return s.cfg.Domain
	}
	return ""
}

func (s *TemplateService) projectOrgID(ctx context.Context, projectID uuid.UUID) (uuid.UUID, error) {
	var p meshdb.Project
	if err := s.db.WithContext(ctx).Select("organization_id").First(&p, "id = ?", projectID).Error; err != nil {
		return uuid.Nil, err
	}
	return p.OrganizationID, nil
}

func (s *TemplateService) findStackService(ctx context.Context, stackID uuid.UUID, name string) *meshdb.Service {
	var svc meshdb.Service
	if err := s.db.WithContext(ctx).First(&svc, "stack_id = ? AND name = ?", stackID, name).Error; err != nil {
		return nil
	}
	return &svc
}

// routeWaitTimeout bounds how long the background route creator waits for a
// service to come up. Long enough for a first image pull on a slow link, short
// enough that a service which never starts stops holding a goroutine.
const routeWaitTimeout = 10 * time.Minute

// createExposedRoutes waits for each exposed service to have a routable
// NodePort, then creates its public route. Run detached from the deploy request.
//
// It polls rather than watching the cluster: the thing it needs is a database
// column that the deployment writes after the rollout, so the database is the
// authority, and a poll keeps this independent of how deployments are driven.
func (s *TemplateService) createExposedRoutes(
	ctx context.Context,
	templateID string,
	stackID, projectID uuid.UUID,
	domainID *uuid.UUID,
	exposes []templates.ResolvedExpose,
) {
	if domainID == nil {
		log.Printf("template %s: no verified base domain — %d exposed service(s) got no route",
			templateID, len(exposes))
		return
	}
	orgID, err := s.projectOrgID(ctx, projectID)
	if err != nil {
		log.Printf("template %s: resolve org for project %s: %v — no routes created", templateID, projectID, err)
		return
	}

	deadline := time.Now().Add(routeWaitTimeout)
	for _, e := range exposes {
		svc := s.findStackService(ctx, stackID, e.Service)
		if svc == nil {
			log.Printf("template %s: exposed service %q is not in the applied stack — no route for %s",
				templateID, e.Service, e.Hostname)
			continue
		}
		if !s.waitForRoutablePort(ctx, svc.ID, deadline) {
			log.Printf("template %s: service %q never got a routable port — no route for %s; create one from the routes tab once it is running",
				templateID, e.Service, e.Hostname)
			continue
		}
		route, err := s.routes.Create(ctx, CreateRouteInput{
			OrgID:     orgID,
			ProjectID: projectID,
			DomainID:  domainID,
			Zone:      meshdb.RouteZonePublic,
			Subdomain: e.Subdomain,
			Targets:   []TargetInput{{ServiceID: &svc.ID, Port: e.Port}},
		})
		if err != nil {
			log.Printf("template %s: create route %s for service %q: %v", templateID, e.Hostname, e.Service, err)
			continue
		}
		// Record the originating stack. Set after creation rather than widening
		// CreateRouteInput, which every other caller shares.
		if route != nil {
			_ = s.db.WithContext(ctx).Model(route).Update("stack_id", stackID).Error
		}
		log.Printf("template %s: route %s created for service %q", templateID, e.Hostname, e.Service)
	}
}

// waitForRoutablePort reports whether the service has a public HTTP port with a
// NodePort assigned, waiting until it does or the deadline passes.
func (s *TemplateService) waitForRoutablePort(ctx context.Context, serviceID uuid.UUID, deadline time.Time) bool {
	for {
		var count int64
		s.db.WithContext(ctx).Model(&meshdb.ServicePort{}).
			Where("service_id = ? AND is_public = true AND is_http = true AND node_port <> 0", serviceID).
			Count(&count)
		if count > 0 {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(3 * time.Second):
		}
	}
}

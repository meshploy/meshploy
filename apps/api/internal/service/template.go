package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/meshploy/apps/api/internal/config"
	"github.com/meshploy/apps/api/internal/templates"
	meshdb "github.com/meshploy/packages/db"
	"gorm.io/gorm"
)

// TemplateService deploys one-click templates. A template is a third stack
// source: Deploy resolves its variables, substitutes them into the compose
// (templates.PrepareSpec — the only deploy-time spec conversion), then lowers to
// the existing stack create + apply + route machinery.
type TemplateService struct {
	db       *gorm.DB
	cfg      *config.Config
	registry *templates.Registry
	stacks   *StackService
	routes   *RouteService
}

// List returns the catalog manifests (empty if no catalog is configured).
func (s *TemplateService) List() ([]*templates.Manifest, error) {
	return s.registry.List()
}

// Get returns a single template.
func (s *TemplateService) Get(id string) (*templates.Template, error) {
	return s.registry.Get(id)
}

// Deploy instantiates templateID into projectID as a stack.
//
// spec, when non-empty, is the (possibly user-edited) compose from the stack
// editor; otherwise the template's own compose is used. promptValues supply the
// prompted variables. The created stack records template provenance, is applied,
// and a public route is created for each exposed service.
func (s *TemplateService) Deploy(ctx context.Context, projectID uuid.UUID, templateID, spec string, promptValues map[string]string, triggeredBy uuid.UUID) (*meshdb.Stack, error) {
	tpl, err := s.registry.Get(templateID)
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

	// Create a public route per exposed service. Best-effort: a missing domain or
	// service should not fail an otherwise-successful deploy.
	if domainID != nil {
		orgID, err := s.projectOrgID(ctx, projectID)
		if err == nil {
			for _, e := range exposes {
				svc := s.findStackService(ctx, stack.ID, e.Service)
				if svc == nil {
					continue
				}
				port := e.Port
				_, _ = s.routes.Create(ctx, CreateRouteInput{
					OrgID:     orgID,
					ProjectID: projectID,
					DomainID:  domainID,
					Zone:      meshdb.RouteZonePublic,
					Subdomain: e.Subdomain,
					Targets:   []TargetInput{{ServiceID: &svc.ID, Port: port}},
				})
			}
		}
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

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
// (templates.PrepareSpec - the only deploy-time spec conversion), then lowers to
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

	// Each exposed service's route is declared in the stack's file, so the
	// apply makes it (and every later apply keeps it), waiting paused for the
	// service's first deploy when it has not run yet.
	if domainID != nil {
		if resolvedSpec, err = declareExposedRoutes(resolvedSpec, exposes); err != nil {
			return nil, err
		}
	} else if len(exposes) > 0 {
		log.Printf("template %s: no verified base domain - %d exposed service(s) declared no route",
			tpl.Manifest.ID, len(exposes))
	}

	stack, err := s.stacks.Create(ctx, projectID, CreateStackInput{
		Name:            s.uniqueStackName(ctx, projectID, tpl.Manifest.ID),
		Spec:            resolvedSpec,
		Variables:       vars,
		TemplateID:      tpl.Manifest.ID,
		TemplateVersion: tpl.Manifest.Version,
	})
	if err != nil {
		return nil, err
	}

	// Reconcile services, volumes and routes from the resolved spec.
	if _, err := s.stacks.Apply(ctx, stack.ID, triggeredBy, nil); err != nil {
		return stack, fmt.Errorf("template deployed but reconcile failed: %w", err)
	}

	return stack, nil
}

// uniqueStackName returns a stack name free within the project, numbering from
// the second instance: "zot", then "zot-2", "zot-3".
//
// Stack names are not unique in the schema, so a second deploy of the same
// template produced two stacks called "zot" - and because a stack's volumes are
// named "<stack>-<volume>", the second silently adopted the first's PVC and both
// wrote to one disk. Numbering is right here rather than a random suffix: this
// is a display name a person reads in a list, the k8s names that must never
// collide are the service slugs, and the read-then-write race only costs a
// duplicate name, not a shared volume.
func (s *TemplateService) uniqueStackName(ctx context.Context, projectID uuid.UUID, base string) string {
	return nextFreeName(base, func(candidate string) bool {
		var count int64
		if err := s.db.WithContext(ctx).Model(&meshdb.Stack{}).
			Where("project_id = ? AND name = ?", projectID, candidate).
			Count(&count).Error; err != nil {
			// Cannot tell. Treat it as free: a duplicate display name is
			// recoverable, refusing to deploy is not.
			return false
		}
		return count > 0
	})
}

// nextFreeName numbers from the second instance: "zot", then "zot-2", "zot-3".
func nextFreeName(base string, taken func(string) bool) string {
	for n := 1; n <= 100; n++ {
		candidate := base
		if n > 1 {
			candidate = fmt.Sprintf("%s-%d", base, n)
		}
		if !taken(candidate) {
			return candidate
		}
	}
	// A hundred instances of one template in one project is not a case worth
	// numbering past; fall back to something certainly free.
	return fmt.Sprintf("%s-%d", base, time.Now().UnixMilli())
}

// orgBaseDomain returns the domain a template's routes go on, the one a
// declared route defaults to (defaultRouteDomain), falling back to the
// configured DOMAIN for the base name when no domain record exists. A nil id
// means no domain-based routing is possible.
func (s *TemplateService) orgBaseDomain(ctx context.Context, projectID uuid.UUID) (*uuid.UUID, string) {
	d, err := defaultRouteDomain(ctx, s.db, projectID)
	if err != nil || d == nil {
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

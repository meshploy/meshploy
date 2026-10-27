package service

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
	appk8s "github.com/meshploy/packages/server/k8s"
	"gorm.io/gorm"
	"k8s.io/client-go/kubernetes"
)

// IsolationService keeps the project namespaces of every organization that
// has switched network isolation on closed to the rest of the cluster, and
// takes its policies away from the others. A namespace is open to:
//
//   - its own pods;
//   - its project's other environment levels, since a level reaches the
//     databases and services of the levels above it;
//   - the projects whose services or jobs use one of its services through
//     that service's published variables: using them is saying it is
//     reached;
//   - the nodes, through which the proxy and published ports arrive.
//
// A project or a level can be opened as an exception, taking traffic from
// anywhere again.
//
// It sweeps every project at start, whenever a switch is flipped, and then on a timer, which picks up what changes the neighbours: a
// variable group attached, a node joining. A new namespace is isolated as it
// is made, before anything runs in it.
type IsolationService struct {
	db  *gorm.DB
	k8s kubernetes.Interface
	mu  sync.Mutex
}

// isolationSweep is how often the policies are brought up to date.
const isolationSweep = time.Minute

func newIsolationService(gdb *gorm.DB, client kubernetes.Interface) *IsolationService {
	s := &IsolationService{db: gdb, k8s: client}
	if client != nil {
		appk8s.OnNamespaceCreated(func(ctx context.Context, _ string) error { return s.Sweep(ctx) })
	}
	return s
}

// Start sweeps now and then every minute, until ctx ends.
func (s *IsolationService) Start(ctx context.Context) {
	if s.k8s == nil {
		return
	}
	for {
		if err := s.Sweep(ctx); err != nil {
			log.Printf("warning: network isolation: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(isolationSweep):
		}
	}
}

// Set switches an organization's isolation on or off and brings the cluster
// in line at once.
func (s *IsolationService) Set(ctx context.Context, orgID uuid.UUID, on bool) error {
	err := s.db.WithContext(ctx).Model(&db.Organization{}).Where("id = ?", orgID).
		Update("network_isolation", on).Error
	if err != nil {
		return err
	}
	return s.Sweep(ctx)
}

// SetProjectOpen exempts a project, or a level, from its organization's
// isolation, or puts it back under it, and brings the cluster in line at
// once.
func (s *IsolationService) SetProjectOpen(ctx context.Context, projectID uuid.UUID, open bool) error {
	err := s.db.WithContext(ctx).Model(&db.Project{}).Where("id = ?", projectID).
		Update("network_open", open).Error
	if err != nil {
		return err
	}
	return s.Sweep(ctx)
}

// Sweep brings every isolated project namespace's policy up to date, and
// removes the policy from every other namespace.
func (s *IsolationService) Sweep(ctx context.Context) error {
	if s.k8s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	isolated, err := s.isolated(ctx)
	if err != nil {
		return err
	}
	have, err := appk8s.IsolatedNamespaces(ctx, s.k8s)
	if err != nil {
		return err
	}
	for ns := range have {
		if !isolated[ns] {
			if err := appk8s.RemoveIsolation(ctx, s.k8s, ns); err != nil {
				return err
			}
		}
	}
	if len(isolated) == 0 {
		return nil
	}
	neighbours, err := s.Neighbours(ctx)
	if err != nil {
		return err
	}
	nodes, err := appk8s.NodeTunnelAddresses(ctx, s.k8s)
	if err != nil {
		return err
	}
	existing, err := appk8s.MeshployNamespaces(ctx, s.k8s)
	if err != nil {
		return err
	}
	for ns := range isolated {
		if !existing[ns] {
			continue // made, and isolated, when its first workload deploys
		}
		if err := appk8s.ApplyIsolation(ctx, s.k8s, ns, appk8s.Isolation{Neighbours: neighbours[ns], NodeAddresses: nodes}); err != nil {
			return err
		}
	}
	return nil
}

// isolated are the namespaces of the projects whose organization has
// isolation on, less the projects opened as an exception.
func (s *IsolationService) isolated(ctx context.Context) (map[string]bool, error) {
	var slugs []string
	err := s.db.WithContext(ctx).Table("projects").
		Joins("JOIN organizations o ON o.id = projects.organization_id").
		Where("o.network_isolation AND NOT projects.network_open").
		Pluck("projects.slug", &slugs).Error
	out := make(map[string]bool, len(slugs))
	for _, slug := range slugs {
		out[slug] = true
	}
	return out, err
}

// Neighbours are, for every project's namespace, the namespaces allowed into
// it besides its own pods and the nodes.
func (s *IsolationService) Neighbours(ctx context.Context) (map[string][]string, error) {
	gdb := s.db.WithContext(ctx)
	var projects []db.Project
	if err := gdb.Select("id", "slug", "parent_project_id").Find(&projects).Error; err != nil {
		return nil, err
	}
	nsOf := make(map[uuid.UUID]string, len(projects))
	family := map[uuid.UUID][]string{}
	rootOf := make(map[uuid.UUID]uuid.UUID, len(projects))
	for _, p := range projects {
		root := p.ID
		if p.ParentProjectID != nil {
			root = *p.ParentProjectID
		}
		nsOf[p.ID], rootOf[p.ID] = p.Slug, root
		family[root] = append(family[root], p.Slug)
	}
	out := make(map[string][]string, len(projects))
	for _, p := range projects {
		out[p.Slug] = append([]string(nil), family[rootOf[p.ID]]...)
	}

	// A service whose published variables another project's service or job
	// uses: (the publisher's project, the user's project).
	var links []struct{ Target, From uuid.UUID }
	if err := gdb.Table("service_variable_groups AS a").
		Select("publisher.project_id AS target, consumer.project_id AS \"from\"").
		Joins("JOIN variable_groups g ON g.id = a.group_id").
		Joins("JOIN services publisher ON publisher.id = g.service_id").
		Joins("JOIN services consumer ON consumer.id = a.service_id").
		Where("publisher.project_id <> consumer.project_id").
		Scan(&links).Error; err != nil {
		return nil, err
	}
	var jobLinks []struct{ Target, From uuid.UUID }
	if err := gdb.Table("job_variable_groups AS a").
		Select("publisher.project_id AS target, consumer.project_id AS \"from\"").
		Joins("JOIN variable_groups g ON g.id = a.group_id").
		Joins("JOIN services publisher ON publisher.id = g.service_id").
		Joins("JOIN jobs consumer ON consumer.id = a.job_id").
		Where("publisher.project_id <> consumer.project_id").
		Scan(&jobLinks).Error; err != nil {
		return nil, err
	}
	for _, l := range append(links, jobLinks...) {
		target, from := nsOf[l.Target], nsOf[l.From]
		if target != "" && from != "" {
			out[target] = append(out[target], from)
		}
	}
	return out, nil
}

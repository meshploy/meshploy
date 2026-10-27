package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
	appk8s "github.com/meshploy/packages/server/k8s"
	"gorm.io/gorm"
	"k8s.io/client-go/kubernetes"
)

// A sandboxed project, or level, runs its apps and jobs on gVisor's user-space
// kernel instead of the host's, so a container escape lands in gVisor rather
// than on the machine. Its managed databases stay on the host's kernel: they
// run the engines' official images, not the project's code, and under gVisor
// a database does about 40% fewer transactions. Each sandboxed pod takes some
// 35 MiB more memory, which is why a project opts in. Builds are never
// sandboxed: BuildKit runs privileged, which gVisor cannot give.

// SandboxRule decides whether a project must be sandboxed, whatever its own
// switch says. why is shown beside the switch it locks.
type SandboxRule func(ctx context.Context, projectID uuid.UUID) (required bool, why string, err error)

var sandboxRules []SandboxRule

// RegisterSandboxRule adds a rule that can require projects to be sandboxed.
// Call it before the server starts.
func RegisterSandboxRule(rule SandboxRule) { sandboxRules = append(sandboxRules, rule) }

// ErrSandboxRequired is returned when a required project is switched off.
var ErrSandboxRequired = errors.New("this project must be sandboxed")

// SandboxService decides which runtime a project's pods use, and switches a
// project's running workloads between them.
type SandboxService struct {
	db  *gorm.DB
	k8s kubernetes.Interface
}

// SandboxState is a project's sandboxing as the console shows it.
type SandboxState struct {
	On       bool   `json:"on"`
	Required bool   `json:"required"`
	Why      string `json:"why,omitempty"`
}

// State is whether a project is sandboxed, and whether a rule requires it.
func (s *SandboxService) State(ctx context.Context, project *db.Project) (SandboxState, error) {
	for _, rule := range sandboxRules {
		required, why, err := rule(ctx, project.ID)
		if err != nil {
			return SandboxState{}, err
		}
		if required {
			return SandboxState{On: true, Required: true, Why: why}, nil
		}
	}
	return SandboxState{On: project.Sandboxed}, nil
}

// RuntimeFor is the runtime class a project's apps and jobs take, with
// nodeName the node a pod is pinned to, if any: "" for the default runtime, or
// SandboxRuntimeClass once the cluster can place the pod. A sandboxed project
// with nowhere to run is an error, so the deploy fails saying so rather than
// leaving its pods pending.
func (s *SandboxService) RuntimeFor(ctx context.Context, projectID uuid.UUID, nodeName string) (string, error) {
	if s == nil {
		return "", nil
	}
	var project db.Project
	if err := s.db.WithContext(ctx).First(&project, "id = ?", projectID).Error; err != nil {
		return "", err
	}
	state, err := s.State(ctx, &project)
	if err != nil || !state.On {
		return "", err
	}
	if s.k8s != nil {
		if err := appk8s.SandboxReady(ctx, s.k8s, nodeName); err != nil {
			return "", err
		}
	}
	return appk8s.SandboxRuntimeClass, nil
}

// Set switches a project, or a level, in or out of the sandbox and moves its
// running apps at once: services roll, a schedule's next run takes the new
// runtime. Its databases stay where they are, on the host's kernel. Switching
// on is refused while no node can run sandboxed pods; switching off, while a
// rule requires it.
func (s *SandboxService) Set(ctx context.Context, projectID uuid.UUID, on bool) error {
	var project db.Project
	if err := s.db.WithContext(ctx).First(&project, "id = ?", projectID).Error; err != nil {
		return err
	}
	if !on {
		for _, rule := range sandboxRules {
			required, why, err := rule(ctx, project.ID)
			if err != nil {
				return err
			}
			if required {
				return fmt.Errorf("%w: %s", ErrSandboxRequired, why)
			}
		}
	}
	if on && s.k8s != nil {
		if err := appk8s.SandboxReady(ctx, s.k8s, ""); err != nil {
			return err
		}
		pinned, err := s.pinnedNodes(ctx, project.ID)
		if err != nil {
			return err
		}
		for _, node := range pinned {
			if err := appk8s.SandboxReady(ctx, s.k8s, node); err != nil {
				return err
			}
		}
	}
	if err := s.db.WithContext(ctx).Model(&project).Update("sandboxed", on).Error; err != nil {
		return err
	}
	if s.k8s == nil {
		return nil
	}
	class := ""
	if on {
		class = appk8s.SandboxRuntimeClass
	}
	databases, err := s.databaseWorkloads(ctx, project.ID)
	if err != nil {
		return err
	}
	return appk8s.SetNamespaceRuntime(ctx, s.k8s, project.Slug, class, databases)
}

// databaseWorkloads are the names of a project's managed databases in the
// cluster.
func (s *SandboxService) databaseWorkloads(ctx context.Context, projectID uuid.UUID) ([]string, error) {
	var services []db.Service
	gdb := s.db.WithContext(ctx)
	if err := gdb.Where("project_id = ? AND type = ?", projectID, db.ServiceTypeDatabase).Find(&services).Error; err != nil {
		return nil, err
	}
	names := make([]string, 0, len(services))
	for _, svc := range services {
		var dc db.DatabaseConfig
		if err := gdb.Where("service_id = ?", svc.ID).Limit(1).Find(&dc).Error; err != nil {
			return nil, err
		}
		names = append(names, databaseWorkloadName(&svc, &dc))
	}
	return names, nil
}

// pinnedNodes are the nodes a project's apps and jobs are pinned to; a
// database is never sandboxed, so its node does not matter.
func (s *SandboxService) pinnedNodes(ctx context.Context, projectID uuid.UUID) ([]string, error) {
	var names []string
	gdb := s.db.WithContext(ctx)
	err := gdb.Model(&db.Node{}).Distinct("name").
		Where("id IN (?) OR id IN (?)",
			gdb.Model(&db.Service{}).Select("node_id").Where("project_id = ? AND type <> ? AND node_id IS NOT NULL", projectID, db.ServiceTypeDatabase),
			gdb.Model(&db.Job{}).Select("node_id").Where("project_id = ? AND node_id IS NOT NULL", projectID)).
		Pluck("name", &names).Error
	return names, err
}

// SandboxOverview is an organization's sandboxing as its Settings show it.
type SandboxOverview struct {
	// Nodes are the cluster's nodes prepared to run sandboxed pods.
	Nodes []string `json:"nodes"`
	// Projects are the sandboxed projects and levels, by a switch or a rule.
	Projects []SandboxedProject `json:"projects"`
}

// SandboxedProject is a sandboxed project or level. ProjectID is the project
// it belongs to, itself for a project; Level names a level, empty for
// production.
type SandboxedProject struct {
	ID        uuid.UUID `json:"id"`
	ProjectID uuid.UUID `json:"project_id"`
	Name      string    `json:"name"`
	Level     string    `json:"level,omitempty"`
	Required  bool      `json:"required"`
}

// Overview lists the organization's sandboxed projects and levels, and the
// nodes that can run them.
func (s *SandboxService) Overview(ctx context.Context, orgID uuid.UUID) (SandboxOverview, error) {
	out := SandboxOverview{Nodes: []string{}, Projects: []SandboxedProject{}}
	var projects []db.Project
	if err := s.db.WithContext(ctx).Where("organization_id = ?", orgID).Order("env_level").Find(&projects).Error; err != nil {
		return out, err
	}
	names := make(map[uuid.UUID]string, len(projects))
	for _, p := range projects {
		names[p.ID] = p.Name
	}
	for i := range projects {
		p := &projects[i]
		state, err := s.State(ctx, p)
		if err != nil {
			return out, err
		}
		if !state.On {
			continue
		}
		entry := SandboxedProject{ID: p.ID, ProjectID: p.ID, Name: p.Name, Required: state.Required}
		if p.ParentProjectID != nil {
			entry.ProjectID = *p.ParentProjectID
			entry.Name = names[*p.ParentProjectID]
			entry.Level = p.EnvName
		}
		out.Projects = append(out.Projects, entry)
	}
	if s.k8s != nil {
		nodes, err := appk8s.GetClusterNodes(ctx, s.k8s)
		if err != nil {
			return out, err
		}
		for _, n := range nodes {
			if appk8s.IsSandboxNode(n.Labels) {
				out.Nodes = append(out.Nodes, n.Name)
			}
		}
	}
	return out, nil
}

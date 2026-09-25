package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
	"gorm.io/gorm"
)

type ProjectService struct {
	db *gorm.DB
}

// ProjectCounts holds per-project resource counts returned alongside the project list.
// Add new fields here as new resource types are introduced — the SQL query in
// ListWithCounts uses a single CASE-based aggregation so adding a field is one line.
type ProjectCounts struct {
	ProjectID        uuid.UUID `gorm:"column:project_id"`
	ServicesCount    int       `gorm:"column:services_count"  json:"services_count"`
	DatabasesCount   int       `gorm:"column:databases_count" json:"databases_count"`
	RoutesCount      int       `gorm:"column:routes_count"    json:"routes_count"`
	VariablesCount   int       `gorm:"column:variables_count" json:"variables_count"` // variable groups, as the Variables tab lists them
	JobsCount        int       `gorm:"column:jobs_count"      json:"jobs_count"`
	StacksCount      int       `gorm:"column:stacks_count"    json:"stacks_count"`
	VolumesCount     int       `gorm:"column:volumes_count"   json:"volumes_count"`
	ConfigFilesCount int       `gorm:"column:config_files_count" json:"config_files_count"`
}

// ProjectWithCounts bundles a project with its resource counts.
type ProjectWithCounts struct {
	db.Project
	ProjectCounts
	// Stats breaks the counts down for the project overview: only a single
	// project's read fills it.
	Stats map[string]map[string]int `json:"stats,omitempty"`
	// Levels are a project's environment levels below production, lowest
	// last, on the list: the workspace overview draws its chain from them.
	Levels []LevelSummary `json:"levels,omitempty"`
}

// LevelSummary names one environment level of a project.
type LevelSummary struct {
	ProjectID uuid.UUID `json:"project_id"`
	Name      string    `json:"name"`
	Level     int       `json:"level"`
}

// ProjectListOptions narrows and orders a project list.
type ProjectListOptions struct {
	Search string // matched against name and slug, ignoring case
	Sort   string // "name" for A to Z; anything else, newest first
}

// likeEscaper escapes LIKE's wildcards, so a search for "a_b" matches only that.
var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

func (s *ProjectService) List(ctx context.Context, orgID uuid.UUID) ([]db.Project, error) {
	projects := make([]db.Project, 0)
	err := s.db.WithContext(ctx).Where("organization_id = ?", orgID).Find(&projects).Error
	return projects, err // levels included: callers here want every namespace
}

// ListWithCounts returns an org's projects with resource counts embedded,
// filtered and ordered by opts. A single SQL aggregation query fetches counts for
// all projects at once, with no N+1.
func (s *ProjectService) ListWithCounts(ctx context.Context, orgID uuid.UUID, opts ProjectListOptions) ([]ProjectWithCounts, error) {
	// Projects only: a project's environment levels are reached through it.
	q := s.db.WithContext(ctx).Where("organization_id = ? AND parent_project_id IS NULL", orgID)
	if term := strings.TrimSpace(opts.Search); term != "" {
		pattern := "%" + likeEscaper.Replace(term) + "%"
		q = q.Where("(name ILIKE ? OR slug ILIKE ?)", pattern, pattern)
	}
	// id breaks ties, so projects created in the same instant keep one order.
	if opts.Sort == "name" {
		q = q.Order("LOWER(name)").Order("id")
	} else {
		q = q.Order("created_at DESC").Order("id")
	}
	projects := make([]db.Project, 0)
	if err := q.Find(&projects).Error; err != nil {
		return nil, err
	}
	if len(projects) == 0 {
		return []ProjectWithCounts{}, nil
	}

	projectIDs := make([]uuid.UUID, len(projects))
	for i, p := range projects {
		projectIDs[i] = p.ID
	}

	// Single aggregation query across services and routes.
	// Extend by adding more COALESCE(sub.xxx_count, 0) columns here.
	var counts []ProjectCounts
	if err := s.db.WithContext(ctx).Raw(`
		SELECT
			p.id AS project_id,
			COALESCE(s.services_count,  0) AS services_count,
			COALESCE(s.databases_count, 0) AS databases_count,
			COALESCE(r.routes_count,    0) AS routes_count,
			COALESCE(vg.variables_count, 0) AS variables_count,
			COALESCE(j.jobs_count,      0) AS jobs_count,
			COALESCE(st.stacks_count,   0) AS stacks_count,
			COALESCE(v.volumes_count,   0) AS volumes_count,
			COALESCE(cf.config_files_count, 0) AS config_files_count
		FROM projects p
		LEFT JOIN (
			SELECT project_id,
				COUNT(*) FILTER (WHERE type = 'application') AS services_count,
				COUNT(*) FILTER (WHERE type = 'database')    AS databases_count
			FROM services
			WHERE project_id IN ?
			GROUP BY project_id
		) s ON s.project_id = p.id
		LEFT JOIN (
			-- The Routes tab lists HTTPS routes and TCP ports together.
			SELECT project_id, COUNT(*) AS routes_count
			FROM (
				SELECT project_id FROM routes WHERE project_id IN ?
				UNION ALL
				SELECT project_id FROM tcp_routes WHERE project_id IN ?
			) all_routes
			GROUP BY project_id
		) r ON r.project_id = p.id
		LEFT JOIN (
			SELECT project_id, COUNT(*) AS variables_count
			FROM variable_groups
			WHERE project_id IN ?
			GROUP BY project_id
		) vg ON vg.project_id = p.id
		LEFT JOIN (
			SELECT project_id, COUNT(*) AS jobs_count
			FROM jobs
			WHERE project_id IN ?
			GROUP BY project_id
		) j ON j.project_id = p.id
		LEFT JOIN (
			SELECT project_id, COUNT(*) AS stacks_count
			FROM stacks
			WHERE project_id IN ?
			GROUP BY project_id
		) st ON st.project_id = p.id
		LEFT JOIN (
			SELECT project_id, COUNT(*) AS volumes_count
			FROM volumes
			WHERE project_id IN ?
			GROUP BY project_id
		) v ON v.project_id = p.id
		LEFT JOIN (
			SELECT project_id, COUNT(*) AS config_files_count
			FROM config_files
			WHERE project_id IN ?
			GROUP BY project_id
		) cf ON cf.project_id = p.id
		WHERE p.id IN ?
	`, projectIDs, projectIDs, projectIDs, projectIDs, projectIDs, projectIDs, projectIDs, projectIDs, projectIDs).Scan(&counts).Error; err != nil {
		// Returned, not dropped: an ignored error here once showed every
		// project as empty after the secrets table was retired.
		return nil, err
	}

	// Index counts by project ID for O(1) lookup.
	countMap := make(map[uuid.UUID]ProjectCounts, len(counts))
	for _, c := range counts {
		countMap[c.ProjectID] = c
	}

	var children []db.Project
	if err := s.db.WithContext(ctx).Select("id", "parent_project_id", "env_name", "env_level").
		Where("parent_project_id IN ?", projectIDs).Order("env_level").Find(&children).Error; err != nil {
		return nil, err
	}
	levels := map[uuid.UUID][]LevelSummary{}
	for _, c := range children {
		levels[*c.ParentProjectID] = append(levels[*c.ParentProjectID], LevelSummary{ProjectID: c.ID, Name: c.EnvName, Level: c.EnvLevel})
	}

	result := make([]ProjectWithCounts, len(projects))
	for i, p := range projects {
		result[i] = ProjectWithCounts{
			Project:       p,
			ProjectCounts: countMap[p.ID],
			Levels:        levels[p.ID],
		}
	}
	return result, nil
}

func (s *ProjectService) Get(ctx context.Context, projectID uuid.UUID) (*db.Project, error) {
	var project db.Project
	err := s.db.WithContext(ctx).First(&project, "id = ?", projectID).Error
	return &project, err
}

func (s *ProjectService) GetWithCounts(ctx context.Context, projectID uuid.UUID) (*ProjectWithCounts, error) {
	project, err := s.Get(ctx, projectID)
	if err != nil {
		return nil, err
	}
	var counts []ProjectCounts
	if err := s.db.WithContext(ctx).Raw(`
		SELECT
			? AS project_id,
			(SELECT COUNT(*) FROM services s WHERE s.project_id = ? AND s.type = 'application') AS services_count,
			(SELECT COUNT(*) FROM services s WHERE s.project_id = ? AND s.type = 'database')    AS databases_count,
			(SELECT COUNT(*) FROM routes r   WHERE r.project_id   = ?)
			+ (SELECT COUNT(*) FROM tcp_routes tr WHERE tr.project_id = ?) AS routes_count,
			(SELECT COUNT(*) FROM variable_groups vg WHERE vg.project_id = ?) AS variables_count,
			(SELECT COUNT(*) FROM jobs j     WHERE j.project_id   = ?) AS jobs_count,
			(SELECT COUNT(*) FROM stacks st  WHERE st.project_id  = ?) AS stacks_count,
			(SELECT COUNT(*) FROM volumes v  WHERE v.project_id   = ?) AS volumes_count,
			(SELECT COUNT(*) FROM config_files cf WHERE cf.project_id = ?) AS config_files_count
	`, projectID, projectID, projectID, projectID, projectID, projectID, projectID, projectID, projectID, projectID).Scan(&counts).Error; err != nil {
		return nil, err
	}
	result := &ProjectWithCounts{Project: *project}
	if len(counts) > 0 {
		result.ProjectCounts = counts[0]
	}
	stats, err := s.Stats(ctx, []uuid.UUID{projectID})
	if err != nil {
		return nil, err
	}
	result.Stats = stats
	return result, nil
}

// Stats breaks each count down the way an overview card does, summed over
// projectIDs: services by status, routes by kind, jobs by how they run. A key
// with nothing in it is left out.
func (s *ProjectService) Stats(ctx context.Context, projectIDs []uuid.UUID) (map[string]map[string]int, error) {
	out := map[string]map[string]int{}
	add := func(kind, key string, n int) {
		if n == 0 {
			return
		}
		if out[kind] == nil {
			out[kind] = map[string]int{}
		}
		out[kind][key] += n
	}
	type row struct {
		Key string
		N   int
	}
	countBy := func(model any, key string, where string, args ...any) ([]row, error) {
		var rows []row
		q := s.db.WithContext(ctx).Model(model).Select(key+" AS key, COUNT(*) AS n").
			Where("project_id IN ?", projectIDs).Where(where, args...)
		if !strings.HasPrefix(key, "'") { // a constant key is one group already
			q = q.Group(key)
		}
		err := q.Scan(&rows).Error
		return rows, err
	}
	queries := []struct {
		kind  string
		model any
		key   string
		where string
		args  []any
	}{
		{"services", &db.Service{}, "status", "type = ?", []any{db.ServiceTypeApplication}},
		{"databases", &db.Service{}, "status", "type = ?", []any{db.ServiceTypeDatabase}},
		{"stacks", &db.Stack{}, "status", "TRUE", nil},
		{"volumes", &db.Volume{}, "status", "TRUE", nil},
		{"routes", &db.Route{}, "CASE WHEN NOT published THEN 'paused' WHEN zone = 'internal' THEN 'internal' ELSE 'https' END", "TRUE", nil},
		{"routes", &db.TCPRoute{}, "'tcp'", "TRUE", nil},
		{"jobs", &db.Job{}, "CASE WHEN schedule <> '' THEN 'scheduled' ELSE 'manual' END", "TRUE", nil},
		{"jobs", &db.Job{}, "'failed'", "status = ?", []any{db.JobStatusFailed}},
		{"variables", &db.VariableGroup{}, "CASE WHEN service_id IS NULL THEN 'shared' ELSE 'published' END", "TRUE", nil},
		{"config_files", &db.ConfigFile{}, "CASE WHEN EXISTS (SELECT 1 FROM service_config_files scf WHERE scf.config_file_id = config_files.id) THEN 'attached' ELSE 'unused' END", "TRUE", nil},
	}
	for _, q := range queries {
		rows, err := countBy(q.model, q.key, q.where, q.args...)
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			add(q.kind, r.Key, r.N)
		}
	}
	return out, nil
}

// reservedNamespaces are namespaces Kubernetes owns. A project's slug is its
// namespace, so a project called "kube-system" would deploy workloads into the
// cluster's own namespace - and deleting that project would take it with them.
// The names are the cluster's, not ours, so the check lives here rather than in
// the handler's pattern.
var reservedNamespaces = map[string]bool{
	"default": true, "kube-system": true, "kube-public": true, "kube-node-lease": true,
	"kubernetes-dashboard": true, "local-path-storage": true,
}

func (s *ProjectService) Create(ctx context.Context, orgID uuid.UUID, name, slug string) (*db.Project, error) {
	if err := checkQuota(ctx, orgID, QuotaProject); err != nil {
		return nil, err
	}
	if reservedNamespaces[slug] {
		return nil, huma.Error422UnprocessableEntity(
			fmt.Sprintf("%q is a Kubernetes namespace and cannot be a project slug", slug))
	}
	project := &db.Project{OrganizationID: orgID, Name: name, Slug: slug}
	return project, s.db.WithContext(ctx).Create(project).Error
}

// DefaultProjectName and DefaultProjectSlug are the project a new install
// starts with, so the console has somewhere to land and the first route or
// import has somewhere to go. Not "default": that is a namespace Kubernetes
// already owns.
const (
	DefaultProjectName = "Apps"
	DefaultProjectSlug = "apps"
)

// CreateDefaultProject makes the starting project for a new organisation,
// inside the caller's transaction. The slug is a namespace and namespaces are
// cluster-wide, so a second organisation on the same install gets a numbered
// one rather than failing to register.
func CreateDefaultProject(tx *gorm.DB, orgID uuid.UUID) error {
	slug := DefaultProjectSlug
	for n := 2; ; n++ {
		var taken int64
		if err := tx.Model(&db.Project{}).Where("slug = ?", slug).Count(&taken).Error; err != nil {
			return err
		}
		if taken == 0 {
			break
		}
		if n > 50 {
			return nil // somebody has fifty of these; they do not need ours
		}
		slug = fmt.Sprintf("%s-%d", DefaultProjectSlug, n)
	}
	return tx.Create(&db.Project{OrganizationID: orgID, Name: DefaultProjectName, Slug: slug}).Error
}

func (s *ProjectService) Update(ctx context.Context, projectID uuid.UUID, name string) (*db.Project, error) {
	var project db.Project
	if err := s.db.WithContext(ctx).First(&project, "id = ?", projectID).Error; err != nil {
		return nil, err
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&project).Update("name", name).Error; err != nil {
			return err
		}
		// Levels carry their project's name, so the console titles them alike.
		if project.ParentProjectID == nil {
			return tx.Model(&db.Project{}).Where("parent_project_id = ?", project.ID).Update("name", name).Error
		}
		return nil
	})
	return &project, err
}

// Delete removes a project, or one environment level of it. A project cannot
// go while it has levels: they are namespaces with their own services, and
// removing a project should never quietly take a staging environment with it.
func (s *ProjectService) Delete(ctx context.Context, projectID uuid.UUID) error {
	project, err := s.Get(ctx, projectID)
	if err != nil {
		return err
	}
	if project.ParentProjectID == nil {
		var levels int64
		if err := s.db.WithContext(ctx).Model(&db.Project{}).
			Where("parent_project_id = ?", projectID).Count(&levels).Error; err != nil {
			return err
		}
		if levels > 0 {
			return ErrProjectHasLevels
		}
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		tx.Where("resource_type = ? AND resource_id = ?", db.ResourceProject, projectID).
			Delete(&db.ResourcePermission{})
		if err := tx.Delete(&db.Project{}, "id = ?", projectID).Error; err != nil {
			return err
		}
		if project.ParentProjectID != nil {
			return closeGap(tx, *project.ParentProjectID, project.EnvLevel)
		}
		return nil
	})
}

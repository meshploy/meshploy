package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
	appk8s "github.com/meshploy/packages/server/k8s"
	"gorm.io/gorm"
)

// Promotion groups and promotion.
//
// A group is a set of services that move up a project's levels together, along
// a path of their own: pg1 might go staging1, staging2, production while pg2
// goes staging1, production. The level a group enters at is the only one where
// its services build; every level above receives the image the one below built
// and ran, deployed as it is, with that level's own variables.
//
// Across levels a service is one lineage and several rows, one per namespace
// (db.Service.LineageID). Databases are never promoted: each level either has
// its own or uses the one above.

type PromotionService struct {
	db          *gorm.DB
	projects    *ProjectService
	deployments *DeploymentService
	workloads   *WorkloadService
	backups     *BackupService
}

var (
	ErrGroupPath        = errors.New("a group's path climbs this project's levels, ending at production")
	ErrGroupEmpty       = errors.New("a group needs at least one service")
	ErrGroupDatabase    = errors.New("databases are not promoted: each level has its own, or uses the one above")
	ErrGroupMember      = errors.New("a service can be in only one group")
	ErrGroupTopLevel    = errors.New("this level is the top of the group's path: there is nowhere to promote to")
	ErrGroupNotHere     = errors.New("this group does not pass through this level")
	ErrNothingToPromote = errors.New("nothing here has been deployed yet, so there is no image to promote")
)

// lineageOf is the service's lineage: the service it was copied from's, or its
// own when it was not copied.
func lineageOf(s db.Service) uuid.UUID {
	if s.LineageID != nil {
		return *s.LineageID
	}
	return s.ID
}

// GroupInput describes a new group. ServiceIDs name services in any level;
// Path lists level project IDs from the entry level up to production.
type GroupInput struct {
	Name       string
	ServiceIDs []uuid.UUID
	Path       []uuid.UUID
	// Single makes the group a copy of one service, which another group can
	// later absorb (see CopyToLevel).
	Single bool
}

// CreateGroup makes a group, puts its services into the level it enters at
// (copying each from the highest level that has it), and leaves building to
// that level alone: auto-deploy is switched off for the group's services on
// every other level of the path, since they now receive promotions instead.
func (s *PromotionService) CreateGroup(ctx context.Context, projectID uuid.UUID, in GroupInput) (*db.PromotionGroup, error) {
	root, err := s.projects.RootProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	levels, err := s.projects.Levels(ctx, root.ID)
	if err != nil {
		return nil, err
	}
	if err := checkPath(in.Path, levels); err != nil {
		return nil, err
	}
	if len(in.ServiceIDs) == 0 {
		return nil, ErrGroupEmpty
	}

	chain := make([]uuid.UUID, len(levels))
	for i, l := range levels {
		chain[i] = l.ProjectID
	}
	var picked []db.Service
	if err := s.db.WithContext(ctx).Where("id IN ? AND project_id IN ?", in.ServiceIDs, chain).Find(&picked).Error; err != nil {
		return nil, err
	}
	if len(picked) != len(in.ServiceIDs) {
		return nil, gorm.ErrRecordNotFound
	}
	lineages := make([]uuid.UUID, 0, len(picked))
	for _, sv := range picked {
		if sv.Type == db.ServiceTypeDatabase {
			return nil, ErrGroupDatabase
		}
		lineages = append(lineages, lineageOf(sv))
	}

	group := &db.PromotionGroup{ProjectID: root.ID, Name: in.Name, Path: pathStrings(in.Path), Single: in.Single}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var taken int64
		if err := tx.Model(&db.PromotionGroupMember{}).
			Where("project_id = ? AND lineage_id IN ?", root.ID, lineages).Count(&taken).Error; err != nil {
			return err
		}
		if taken > 0 {
			return ErrGroupMember
		}
		if err := tx.Create(group).Error; err != nil {
			return err
		}
		for _, l := range lineages {
			if err := tx.Create(&db.PromotionGroupMember{GroupID: group.ID, ProjectID: root.ID, LineageID: l}).Error; err != nil {
				return err
			}
		}
		entry := in.Path[0]
		for _, l := range lineages {
			if _, err := s.ensureInLevel(ctx, tx, l, entry, levels); err != nil {
				return err
			}
		}
		// Only the entry level builds.
		return tx.Model(&db.BuildConfig{}).
			Where("service_id IN (?)", tx.Model(&db.Service{}).Select("id").
				Where("project_id IN ? AND project_id <> ? AND COALESCE(lineage_id, id) IN ?", in.Path, entry, lineages)).
			Update("auto_deploy", false).Error
	})
	if err != nil {
		return nil, err
	}
	return group, nil
}

// checkPath accepts a path that climbs: each level above the last, ending at
// production.
func checkPath(path []uuid.UUID, levels []EnvironmentLevel) error {
	if len(path) < 2 {
		return ErrGroupPath
	}
	at := make(map[uuid.UUID]int, len(levels))
	for _, l := range levels {
		at[l.ProjectID] = l.Level
	}
	prev := -1
	for i, id := range path {
		lvl, ok := at[id]
		if !ok {
			return ErrGroupPath
		}
		if i > 0 && lvl >= prev {
			return ErrGroupPath
		}
		prev = lvl
	}
	if prev != 0 {
		return ErrGroupPath
	}
	return nil
}

func pathStrings(ids []uuid.UUID) db.StringArray {
	out := make(db.StringArray, len(ids))
	for i, id := range ids {
		out[i] = id.String()
	}
	return out
}

func pathIDs(p db.StringArray) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(p))
	for _, s := range p {
		if id, err := uuid.Parse(s); err == nil {
			out = append(out, id)
		}
	}
	return out
}

// ensureInLevel returns the lineage's service in level, copying it there from
// the highest level that has it when it is not there yet.
func (s *PromotionService) ensureInLevel(ctx context.Context, tx *gorm.DB, lineage, level uuid.UUID, levels []EnvironmentLevel) (*db.Service, error) {
	var existing db.Service
	err := tx.WithContext(ctx).Where("project_id = ? AND COALESCE(lineage_id, id) = ?", level, lineage).First(&existing).Error
	if err == nil {
		return &existing, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	// levels runs production first, so the first copy found is the highest.
	for _, l := range levels {
		var src db.Service
		if err := tx.WithContext(ctx).Preload("Ports").Preload("BuildConfig").
			Where("project_id = ? AND COALESCE(lineage_id, id) = ?", l.ProjectID, lineage).
			First(&src).Error; err == nil {
			return copyService(ctx, tx, src, level)
		}
	}
	return nil, gorm.ErrRecordNotFound
}

// copyService puts a copy of src's definition into another level: how it is
// built and run, its ports and variables. Not its deployments, deploy token,
// or anything that recorded what happened to the original.
func copyService(ctx context.Context, tx *gorm.DB, src db.Service, level uuid.UUID) (*db.Service, error) {
	lineage := lineageOf(src)
	dst := db.Service{
		ProjectID:                  level,
		Name:                       src.Name,
		Type:                       src.Type,
		Slug:                       src.Slug,
		Image:                      src.Image,
		PullRegistryIntegrationID:  src.PullRegistryIntegrationID,
		Status:                     db.ServiceStopped,
		Replicas:                   src.Replicas,
		CPURequest:                 src.CPURequest,
		CPULimit:                   src.CPULimit,
		MemoryRequest:              src.MemoryRequest,
		MemoryLimit:                src.MemoryLimit,
		EnvVars:                    src.EnvVars,
		HealthcheckCmd:             src.HealthcheckCmd,
		HealthcheckIntervalSecs:    src.HealthcheckIntervalSecs,
		HealthcheckTimeoutSecs:     src.HealthcheckTimeoutSecs,
		HealthcheckRetries:         src.HealthcheckRetries,
		HealthcheckStartPeriodSecs: src.HealthcheckStartPeriodSecs,
		Command:                    src.Command,
		Args:                       src.Args,
		StartCommand:               src.StartCommand,
		LineageID:                  &lineage,
	}
	if err := tx.WithContext(ctx).Create(&dst).Error; err != nil {
		return nil, err
	}
	for _, p := range src.Ports {
		port := db.ServicePort{ServiceID: dst.ID, Name: p.Name, Port: p.Port, IsHTTP: p.IsHTTP, IsPrimary: p.IsPrimary, IsPublic: p.IsPublic}
		if err := tx.WithContext(ctx).Create(&port).Error; err != nil {
			return nil, err
		}
		dst.Ports = append(dst.Ports, port)
	}
	// Its attached variable groups come along as they are. A group another
	// service publishes is resolved at deploy time to the nearest copy of that
	// service (see VariableGroupService.nearestCopies), and any other group is
	// shared with the level it came from until this level has its own: a level
	// uses what it does not have from above.
	var attached []db.ServiceVariableGroup
	if err := tx.WithContext(ctx).Where("service_id = ?", src.ID).Find(&attached).Error; err != nil {
		return nil, err
	}
	for _, a := range attached {
		if err := tx.WithContext(ctx).Create(&db.ServiceVariableGroup{ServiceID: dst.ID, GroupID: a.GroupID}).Error; err != nil {
			return nil, err
		}
	}
	var levelProject db.Project
	if err := tx.WithContext(ctx).First(&levelProject, "id = ?", level).Error; err != nil {
		return nil, err
	}
	if err := copyRoutes(ctx, tx, src.ID, &dst, levelProject); err != nil {
		return nil, err
	}
	if bc := src.BuildConfig; bc != nil {
		bcCopy := db.BuildConfig{
			ServiceID:             dst.ID,
			Builder:               bc.Builder,
			GitIntegrationID:      bc.GitIntegrationID,
			GitRepo:               bc.GitRepo,
			Branch:                bc.Branch,
			RootDir:               bc.RootDir,
			DockerfilePath:        bc.DockerfilePath,
			InstallCommand:        bc.InstallCommand,
			BuildCommand:          bc.BuildCommand,
			BuildArgs:             bc.BuildArgs,
			BuildEnvVars:          bc.BuildEnvVars,
			BuilderNode:           bc.BuilderNode,
			BuilderCPURequest:     bc.BuilderCPURequest,
			BuilderMemoryRequest:  bc.BuilderMemoryRequest,
			BuilderCPULimit:       bc.BuilderCPULimit,
			BuilderMemoryLimit:    bc.BuilderMemoryLimit,
			RegistryIntegrationID: bc.RegistryIntegrationID,
			RollbackEnabled:       bc.RollbackEnabled,
			ImageRetention:        bc.ImageRetention,
			AutoDeploy:            bc.AutoDeploy,
			WatchPaths:            bc.WatchPaths,
		}
		if err := tx.WithContext(ctx).Create(&bcCopy).Error; err != nil {
			return nil, err
		}
	}
	return &dst, nil
}

// ListGroups returns a project's groups, oldest first.
func (s *PromotionService) ListGroups(ctx context.Context, projectID uuid.UUID) ([]GroupView, error) {
	root, err := s.projects.RootProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	var groups []db.PromotionGroup
	if err := s.db.WithContext(ctx).Where("project_id = ?", root.ID).Order("created_at").Order("id").Find(&groups).Error; err != nil {
		return nil, err
	}
	var members []db.PromotionGroupMember
	if err := s.db.WithContext(ctx).Where("project_id = ?", root.ID).Find(&members).Error; err != nil {
		return nil, err
	}
	out := make([]GroupView, len(groups))
	for i, g := range groups {
		out[i] = GroupView{PromotionGroup: g, Lineages: []uuid.UUID{}}
		for _, m := range members {
			if m.GroupID == g.ID {
				out[i].Lineages = append(out[i].Lineages, m.LineageID)
			}
		}
	}
	return out, nil
}

// GroupView is a group with the lineages in it.
type GroupView struct {
	db.PromotionGroup
	Lineages []uuid.UUID `json:"lineages"`
}

// DeleteGroup removes a group. Its services stay where they are, in every
// level; they simply stop moving together.
func (s *PromotionService) DeleteGroup(ctx context.Context, projectID, groupID uuid.UUID) error {
	root, err := s.projects.RootProject(ctx, projectID)
	if err != nil {
		return err
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var group db.PromotionGroup
		if err := tx.Where("id = ? AND project_id = ?", groupID, root.ID).First(&group).Error; err != nil {
			return err
		}
		var lineages []uuid.UUID
		if err := tx.Model(&db.PromotionGroupMember{}).Where("group_id = ?", groupID).Pluck("lineage_id", &lineages).Error; err != nil {
			return err
		}
		if err := releaseLineages(tx, pathIDs(group.Path), lineages); err != nil {
			return err
		}
		if err := tx.Delete(&group).Error; err != nil {
			return err
		}
		return tx.Where("group_id = ?", groupID).Delete(&db.PromotionGroupMember{}).Error
	})
}

// NextLevel is where Promote would send a group from level: the next level on
// its path. The handler checks the caller may change that level too, since
// promoting from staging is deploying to production.
func (s *PromotionService) NextLevel(ctx context.Context, level, groupID uuid.UUID) (uuid.UUID, error) {
	root, err := s.projects.RootProject(ctx, level)
	if err != nil {
		return uuid.Nil, err
	}
	var group db.PromotionGroup
	if err := s.db.WithContext(ctx).Where("id = ? AND project_id = ?", groupID, root.ID).First(&group).Error; err != nil {
		return uuid.Nil, err
	}
	path := pathIDs(group.Path)
	for i, id := range path {
		if id == level {
			if i == len(path)-1 {
				return uuid.Nil, ErrGroupTopLevel
			}
			return path[i+1], nil
		}
	}
	return uuid.Nil, ErrGroupNotHere
}

// Promotion is one service's move up a level.
type Promotion struct {
	ServiceName  string        `json:"service_name"`
	FromService  uuid.UUID     `json:"from_service_id"`
	ToService    uuid.UUID     `json:"to_service_id"`
	Image        string        `json:"image"`
	Deployment   db.Deployment `json:"deployment"`
	CreatedThere bool          `json:"created_there"`
}

// Skipped is a service Promote left where it was, and why.
type Skipped struct {
	ServiceName string `json:"service_name"`
	// Reason is one of: unchanged, older, never_built, not_here, and
	// from_below, for one whose variables would come from below the target.
	Reason string `json:"reason"`
	// Detail says what the target needs first, for from_below.
	Detail string `json:"detail,omitempty"`
}

// PromoteResult is what a promotion moved, and what it left.
type PromoteResult struct {
	Promoted []Promotion `json:"promoted"`
	Skipped  []Skipped   `json:"skipped"`
}

// Promote moves a group's services from level to the next level on the
// group's path: each one's current image, from its last successful deployment
// here, deployed as it is up there, and copied there first when it is not there
// yet. Only what is newer here moves: a service running the same image as the
// level above, one whose image here was built before what runs above, one never
// built here, and one not in this level are all left where they are, and
// reported as skipped with the reason. A promotion that would move nothing is
// refused.
func (s *PromotionService) Promote(ctx context.Context, level, groupID uuid.UUID) (*PromoteResult, error) {
	return s.promote(ctx, level, groupID, false)
}

// Overwrite is Promote for a level above that runs something of its own, a
// hotfix built there: it moves each image that differs from what the level
// above runs, older or not. The dialog asking for it names what is replaced.
func (s *PromotionService) Overwrite(ctx context.Context, level, groupID uuid.UUID) (*PromoteResult, error) {
	return s.promote(ctx, level, groupID, true)
}

func (s *PromotionService) promote(ctx context.Context, level, groupID uuid.UUID, overwrite bool) (*PromoteResult, error) {
	root, err := s.projects.RootProject(ctx, level)
	if err != nil {
		return nil, err
	}
	var group db.PromotionGroup
	if err := s.db.WithContext(ctx).Where("id = ? AND project_id = ?", groupID, root.ID).First(&group).Error; err != nil {
		return nil, err
	}
	path := pathIDs(group.Path)
	next := uuid.Nil
	for i, id := range path {
		if id == level {
			if i == len(path)-1 {
				return nil, ErrGroupTopLevel
			}
			next = path[i+1]
		}
	}
	if next == uuid.Nil {
		return nil, ErrGroupNotHere
	}
	levels, err := s.projects.Levels(ctx, root.ID)
	if err != nil {
		return nil, err
	}
	var from, to string
	for _, l := range levels {
		if l.ProjectID == level {
			from = l.Name
		}
		if l.ProjectID == next {
			to = l.Name
		}
	}

	var members []db.PromotionGroupMember
	if err := s.db.WithContext(ctx).Where("group_id = ?", group.ID).Find(&members).Error; err != nil {
		return nil, err
	}

	latest := func(serviceID uuid.UUID) (*db.Deployment, error) {
		var d db.Deployment
		err := s.db.WithContext(ctx).
			Where("service_id = ? AND status = ? AND image <> ''", serviceID, db.DeploymentSuccess).
			Order("created_at DESC").First(&d).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return &d, err
	}

	type move struct {
		src   db.Service
		image string
		depID uuid.UUID
	}
	var moves []move
	result := &PromoteResult{Promoted: []Promotion{}, Skipped: []Skipped{}}
	for _, m := range members {
		var src db.Service
		if err := s.db.WithContext(ctx).Where("project_id = ? AND COALESCE(lineage_id, id) = ?", level, m.LineageID).First(&src).Error; err != nil {
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, err
			}
			var elsewhere db.Service
			if s.db.WithContext(ctx).Where("COALESCE(lineage_id, id) = ?", m.LineageID).First(&elsewhere).Error == nil {
				result.Skipped = append(result.Skipped, Skipped{ServiceName: elsewhere.Name, Reason: "not_here"})
			}
			continue
		}
		here, err := latest(src.ID)
		if err != nil {
			return nil, err
		}
		if here == nil {
			result.Skipped = append(result.Skipped, Skipped{ServiceName: src.Name, Reason: "never_built"})
			continue
		}
		var up db.Service
		err = s.db.WithContext(ctx).Where("project_id = ? AND COALESCE(lineage_id, id) = ?", next, m.LineageID).First(&up).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		if err == nil {
			above, err := latest(up.ID)
			if err != nil {
				return nil, err
			}
			if above != nil && above.Image == here.Image {
				result.Skipped = append(result.Skipped, Skipped{ServiceName: src.Name, Reason: "unchanged"})
				continue
			}
			// Ordered by when each image was built, not when it was last
			// deployed: a rollback or redeploy above makes nothing newer.
			if above != nil && !overwrite &&
				!s.deployments.ImageOrigin(ctx, *here).BuiltAt.After(s.deployments.ImageOrigin(ctx, *above).BuiltAt) {
				result.Skipped = append(result.Skipped, Skipped{ServiceName: src.Name, Reason: "older"})
				continue
			}
		}
		// Its variables as the target would resolve them: the target's copy's
		// when it has one, else the ones the copy will be made with.
		attached := src.ID
		if up.ID != uuid.Nil {
			attached = up.ID
		}
		if err := s.deployments.varGroups.CheckBorrowingAt(ctx, next, attached); err != nil {
			if !errors.Is(err, ErrBorrowFromBelow) {
				return nil, err
			}
			result.Skipped = append(result.Skipped, Skipped{ServiceName: src.Name, Reason: "from_below", Detail: fromBelowDetail(err)})
			continue
		}
		moves = append(moves, move{src: src, image: here.Image, depID: here.ID})
	}
	if len(moves) == 0 {
		return nil, fmt.Errorf("%w: nothing in %s is newer than what %s runs", ErrNothingToPromote, from, to)
	}

	for _, mv := range moves {
		var target *db.Service
		created := false
		err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var existing db.Service
			err := tx.Where("project_id = ? AND COALESCE(lineage_id, id) = ?", next, lineageOf(mv.src)).First(&existing).Error
			if err == nil {
				target = &existing
				return nil
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			var src db.Service
			if err := tx.Preload("Ports").Preload("BuildConfig").First(&src, "id = ?", mv.src.ID).Error; err != nil {
				return err
			}
			copied, err := copyService(ctx, tx, src, next)
			if err != nil {
				return err
			}
			// Above the entry level a service receives promotions and never
			// builds on a push of its own.
			if err := tx.Model(&db.BuildConfig{}).Where("service_id = ?", copied.ID).Update("auto_deploy", false).Error; err != nil {
				return err
			}
			target, created = copied, true
			return nil
		})
		if err != nil {
			return result, err
		}
		var fromDep db.Deployment
		_ = s.db.WithContext(ctx).First(&fromDep, "id = ?", mv.depID).Error
		dep, err := s.deployments.DeployImage(ctx, target.ID, mv.image,
			fmt.Sprintf("Promoted from %s (deployment %s, %s)", from, mv.depID.String()[:8], mv.image),
			Provenance{Source: db.DeploySourcePromotion, FromLevel: from, From: &fromDep})
		if err != nil {
			return result, fmt.Errorf("promote %s to %s: %w", mv.src.Name, to, err)
		}
		result.Promoted = append(result.Promoted, Promotion{
			ServiceName:  mv.src.Name,
			FromService:  mv.src.ID,
			ToService:    target.ID,
			Image:        mv.image,
			Deployment:   *dep,
			CreatedThere: created,
		})
	}
	return result, nil
}

// Board is what the project's overview shows: its levels, and for each group
// what each of its services runs at each level of the path.
type Board struct {
	Levels []EnvironmentLevel `json:"levels"`
	Groups []BoardGroup       `json:"groups"`
	// Ungrouped are services in no group, by level: they do not move.
	Ungrouped []BoardCell `json:"ungrouped"`
}

type BoardGroup struct {
	ID    uuid.UUID   `json:"id"`
	Name  string      `json:"name"`
	Path  []uuid.UUID `json:"path"`
	Cells []BoardCell `json:"cells"`
}

// BoardCell is one service at one level.
type BoardCell struct {
	LevelID     uuid.UUID  `json:"level_id"`
	LineageID   uuid.UUID  `json:"lineage_id"`
	ServiceID   uuid.UUID  `json:"service_id"`
	ServiceName string     `json:"service_name"`
	Type        string     `json:"type"`
	Status      string     `json:"status"`
	Image       string     `json:"image"`
	DeployedAt  *time.Time `json:"deployed_at,omitempty"`
	// HasBackup is set on a database whose last backup succeeded: one a
	// level's own copy can be cloned from.
	HasBackup bool `json:"has_backup,omitempty"`
	// Where the running image came from: built here from a branch, or moved
	// here from another level, carrying the commit it was built from.
	Source              string `json:"source,omitempty"`
	SourceBranch        string `json:"source_branch,omitempty"`
	SourceCommit        string `json:"source_commit,omitempty"`
	SourceCommitMessage string `json:"source_commit_message,omitempty"`
	FromLevel           string `json:"from_level,omitempty"`
	// Arrival and ImageBuiltAt are where the running image came from, looked
	// through redeploys and rollbacks, and when it was built: what Promote
	// compares, and what says a level runs something built there instead of
	// something promoted to it.
	Arrival      string     `json:"arrival,omitempty"`
	ImageBuiltAt *time.Time `json:"image_built_at,omitempty"`
	// Routes are the service's public addresses at this level, published
	// ones first, so the card can open what this level serves.
	Routes []BoardRoute `json:"routes,omitempty"`
}

// BoardRoute is one address a card links to. Live is false for a route that
// is paused, or copied into the level and waiting for its first deploy.
type BoardRoute struct {
	Hostname string `json:"hostname"`
	Live     bool   `json:"live"`
}

// Board assembles the overview's view of a project.
func (s *PromotionService) Board(ctx context.Context, projectID uuid.UUID) (*Board, error) {
	levels, err := s.projects.Levels(ctx, projectID)
	if err != nil {
		return nil, err
	}
	groups, err := s.ListGroups(ctx, projectID)
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, len(levels))
	for i, l := range levels {
		ids[i] = l.ProjectID
	}
	var services []db.Service
	if err := s.db.WithContext(ctx).Where("project_id IN ?", ids).Order("name").Find(&services).Error; err != nil {
		return nil, err
	}
	serviceIDs := make([]uuid.UUID, len(services))
	for i, sv := range services {
		serviceIDs[i] = sv.ID
	}
	// The image each service last deployed successfully.
	var deps []db.Deployment
	if len(serviceIDs) > 0 {
		if err := s.db.WithContext(ctx).
			Where("service_id IN ? AND status = ? AND image <> ''", serviceIDs, db.DeploymentSuccess).
			Order("created_at DESC").Find(&deps).Error; err != nil {
			return nil, err
		}
	}
	latest := map[uuid.UUID]db.Deployment{}
	for _, d := range deps {
		if _, seen := latest[d.ServiceID]; !seen {
			latest[d.ServiceID] = d
		}
	}

	var backedUp []uuid.UUID
	if len(serviceIDs) > 0 {
		if err := s.db.WithContext(ctx).Model(&db.BackupConfig{}).
			Where("service_id IN ? AND last_backup_status = ?", serviceIDs, db.BackupSuccess).
			Pluck("service_id", &backedUp).Error; err != nil {
			return nil, err
		}
	}
	hasBackup := make(map[uuid.UUID]bool, len(backedUp))
	for _, id := range backedUp {
		hasBackup[id] = true
	}

	// Each service's addresses, internal ones left out: nobody opens those
	// from a browser.
	var routeRows []struct {
		ServiceID      uuid.UUID
		Hostname       string
		Published      bool
		AwaitingDeploy bool
	}
	if len(serviceIDs) > 0 {
		if err := s.db.WithContext(ctx).Model(&db.Route{}).
			Select("DISTINCT route_targets.service_id, routes.hostname, routes.published, routes.awaiting_deploy").
			Joins("JOIN route_targets ON route_targets.route_id = routes.id").
			Where("route_targets.service_id IN ? AND routes.zone <> ?", serviceIDs, db.RouteZoneInternal).
			Order("routes.hostname").Scan(&routeRows).Error; err != nil {
			return nil, err
		}
	}
	routesOf := map[uuid.UUID][]BoardRoute{}
	for _, r := range routeRows {
		routesOf[r.ServiceID] = append(routesOf[r.ServiceID], BoardRoute{Hostname: r.Hostname, Live: r.Published && !r.AwaitingDeploy})
	}
	for id := range routesOf {
		sort.SliceStable(routesOf[id], func(a, b int) bool { return routesOf[id][a].Live && !routesOf[id][b].Live })
	}

	grouped := map[uuid.UUID]int{}
	board := &Board{Levels: levels, Groups: make([]BoardGroup, len(groups)), Ungrouped: []BoardCell{}}
	for i, g := range groups {
		board.Groups[i] = BoardGroup{ID: g.ID, Name: g.Name, Path: pathIDs(g.Path), Cells: []BoardCell{}}
		for _, l := range g.Lineages {
			grouped[l] = i
		}
	}
	for _, sv := range services {
		cell := BoardCell{
			LevelID:     sv.ProjectID,
			LineageID:   lineageOf(sv),
			ServiceID:   sv.ID,
			ServiceName: sv.Name,
			Type:        string(sv.Type),
			Status:      string(sv.Status),
			HasBackup:   hasBackup[sv.ID],
			Routes:      routesOf[sv.ID],
		}
		if d, ok := latest[sv.ID]; ok {
			cell.Image, cell.DeployedAt = d.Image, d.DeployedAt
			cell.Source, cell.SourceBranch, cell.FromLevel = d.Source, d.SourceBranch, d.FromLevel
			cell.SourceCommit, cell.SourceCommitMessage = d.SourceCommit, d.SourceCommitMessage
			origin := s.deployments.ImageOrigin(ctx, d)
			cell.Arrival, cell.ImageBuiltAt = origin.Arrival, &origin.BuiltAt
		}
		if gi, ok := grouped[cell.LineageID]; ok {
			board.Groups[gi].Cells = append(board.Groups[gi].Cells, cell)
		} else {
			board.Ungrouped = append(board.Ungrouped, cell)
		}
	}
	sort.SliceStable(board.Ungrouped, func(a, b int) bool { return board.Ungrouped[a].ServiceName < board.Ungrouped[b].ServiceName })
	return board, nil
}

// copyRoutes gives a service copied into a level the routes its original has,
// under the level's derived names (app becomes app-staging), paused until the
// copy's first successful deploy there publishes them. A target on another
// service keeps pointing where it did: the level uses that service from above.
// A custom hostname cannot take a suffix, so its copy moves to the primary
// domain as <first label>-<level>. Redirects are not copied, and neither is a
// route whose derived name is already routed.
func copyRoutes(ctx context.Context, tx *gorm.DB, srcID uuid.UUID, dst *db.Service, level db.Project) error {
	var routes []db.Route
	if err := tx.WithContext(ctx).Preload("Targets").
		Where("id IN (?)", tx.Model(&db.RouteTarget{}).Select("route_id").Where("service_id = ?", srcID)).
		Find(&routes).Error; err != nil {
		return err
	}
	name := level.EnvName
	if level.ParentProjectID == nil {
		name = "" // copied back up into production: the real names
	}
routes:
	for _, r := range routes {
		for _, t := range r.Targets {
			if t.RedirectRouteID != nil {
				continue routes
			}
		}
		domainID, zone, sub := r.DomainID, r.Zone, r.Subdomain
		if domainID == nil {
			var primary db.Domain
			if err := tx.WithContext(ctx).Where("organization_id = ? AND is_primary = ?", r.OrganizationID, true).
				First(&primary).Error; err != nil {
				continue // no base domain to derive a name on
			}
			domainID, zone, sub = &primary.ID, db.RouteZonePublic, strings.SplitN(r.Hostname, ".", 2)[0]
		}
		var domain db.Domain
		if err := tx.WithContext(ctx).First(&domain, "id = ?", *domainID).Error; err != nil {
			continue
		}
		hostname := hostnameFor(zone, levelSubdomain(sub, name), &domain)
		var taken int64
		if err := tx.WithContext(ctx).Model(&db.Route{}).Where("hostname = ?", hostname).Count(&taken).Error; err != nil {
			return err
		}
		if taken > 0 {
			continue
		}
		copied := db.Route{
			OrganizationID: r.OrganizationID,
			ProjectID:      level.ID,
			DomainID:       domainID,
			Zone:           zone,
			Subdomain:      sub,
			Hostname:       hostname,
		}
		if err := tx.WithContext(ctx).Create(&copied).Error; err != nil {
			return err
		}
		// GORM writes a false bool with a default as the default, so the pause
		// is its own update.
		if err := tx.WithContext(ctx).Model(&copied).Updates(map[string]any{"published": false, "awaiting_deploy": true}).Error; err != nil {
			return err
		}
		for _, t := range r.Targets {
			nt := db.RouteTarget{RouteID: copied.ID, Path: t.Path, StripPath: t.StripPath,
				ServiceID: t.ServiceID, NodeID: t.NodeID, TargetIP: t.TargetIP, TargetPort: t.TargetPort,
				TargetTLS: t.TargetTLS, RedirectCode: t.RedirectCode}
			if t.ServiceID != nil && *t.ServiceID == srcID {
				// This level's own copy, whose address is not known until it runs.
				nt.ServiceID, nt.TargetIP, nt.TargetPort = &dst.ID, "", 0
			}
			if err := tx.WithContext(ctx).Create(&nt).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

// ErrHasOwnDatabase refuses a second copy of a database in a level.
var ErrHasOwnDatabase = errors.New("this level already has its own copy of that database")

// OwnDatabase gives a level its own copy of a database it currently uses from
// a level above: same engine, version, size, database name and user, with its
// own password and its own volume, and nothing shared. The level's services
// switch to it on their next deploy, since a service uses the nearest copy.
//
// With clone, the source's latest backup is restored into it once it is up: a
// level that starts with production's data. Refused when the source has no
// backup, rather than started empty under a name that says clone. The restore
// runs in the background, and its outcome is written to the new database's
// deploy log.
func (s *PromotionService) OwnDatabase(ctx context.Context, level, sourceID uuid.UUID, clone bool) (*db.Service, error) {
	root, err := s.projects.RootProject(ctx, level)
	if err != nil {
		return nil, err
	}
	var src db.Service
	if err := s.db.WithContext(ctx).First(&src, "id = ?", sourceID).Error; err != nil {
		return nil, err
	}
	var srcProject db.Project
	if err := s.db.WithContext(ctx).First(&srcProject, "id = ?", src.ProjectID).Error; err != nil {
		return nil, err
	}
	if srcProject.ID != root.ID && (srcProject.ParentProjectID == nil || *srcProject.ParentProjectID != root.ID) {
		return nil, gorm.ErrRecordNotFound
	}
	if src.Type != db.ServiceTypeDatabase {
		return nil, fmt.Errorf("%s is not a database", src.Name)
	}
	lineage := lineageOf(src)
	var existing int64
	if err := s.db.WithContext(ctx).Model(&db.Service{}).
		Where("project_id = ? AND COALESCE(lineage_id, id) = ?", level, lineage).Count(&existing).Error; err != nil {
		return nil, err
	}
	if existing > 0 {
		return nil, ErrHasOwnDatabase
	}
	var dc db.DatabaseConfig
	if err := s.db.WithContext(ctx).First(&dc, "service_id = ?", src.ID).Error; err != nil {
		return nil, err
	}

	var backupCfg uuid.UUID
	var backup BackupObject
	if clone {
		if s.backups == nil {
			return nil, ErrNoBackup
		}
		backupCfg, backup, err = s.backups.LatestBackup(ctx, src.ID)
		if err != nil {
			return nil, fmt.Errorf("%w: %s has none to clone from. Take one first, or start empty", ErrNoBackup, src.Name)
		}
	}

	own, err := s.workloads.Create(ctx, level, CreateWorkloadInput{
		Name:          src.Name,
		NodeID:        nil, // placed by the cluster; the source's node belongs to its level
		Type:          db.ServiceTypeDatabase,
		Engine:        dc.Engine,
		Version:       dc.Version,
		StorageGB:     dc.StorageGB,
		DBName:        dc.DBName,
		DBUser:        dc.DBUser,
		CPURequest:    src.CPURequest,
		CPULimit:      src.CPULimit,
		MemoryRequest: src.MemoryRequest,
		MemoryLimit:   src.MemoryLimit,
	})
	if err != nil {
		return nil, err
	}
	if err := s.db.WithContext(ctx).Model(own).Update("lineage_id", lineage).Error; err != nil {
		return nil, err
	}
	own.LineageID = &lineage

	dep, err := s.deployments.Trigger(ctx, TriggerInput{ServiceID: own.ID})
	if err != nil {
		return own, err
	}
	if clone {
		go s.cloneWhenUp(own.ID, dep.ID, backupCfg, backup.Key, src.Name)
	}
	return own, nil
}

// cloneWhenUp waits for a new database's first deploy to finish, then restores
// the backup into it once. Not retried: a restore that failed partway and ran
// again would collide with what it had already written.
func (s *PromotionService) cloneWhenUp(serviceID, deploymentID, backupCfg uuid.UUID, key, sourceName string) {
	ctx := context.Background()
	note := func(msg string) {
		s.db.Model(&db.Deployment{}).Where("id = ?", deploymentID).Update("log", gorm.Expr("log || ?", msg+"\n"))
	}
	deadline := time.Now().Add(15 * time.Minute)
	for {
		var dep db.Deployment
		if err := s.db.First(&dep, "id = ?", deploymentID).Error; err != nil {
			return
		}
		if dep.Status == db.DeploymentSuccess {
			break
		}
		if dep.Status == db.DeploymentFailed || time.Now().After(deadline) {
			note("Clone from " + sourceName + " not started: the database did not come up.")
			return
		}
		time.Sleep(5 * time.Second)
	}
	// A database accepts connections a little after its pod runs.
	time.Sleep(15 * time.Second)
	note("Cloning from " + sourceName + "'s backup " + key + "...")
	if err := s.backups.RestoreInto(ctx, backupCfg, key, serviceID); err != nil {
		note("Clone failed: " + err.Error())
		return
	}
	note("Cloned from " + sourceName + "'s backup " + key + ".")
}

// ── Editing groups ───────────────────────────────────────────────────────────

var (
	ErrNotBelow   = errors.New("copies and bring-downs go to a level below the service's own")
	ErrNotInGroup = errors.New("that service is not in this group")
)

// loadGroup returns a group of the project projectID belongs to.
func (s *PromotionService) loadGroup(ctx context.Context, projectID, groupID uuid.UUID) (*db.PromotionGroup, *db.Project, error) {
	root, err := s.projects.RootProject(ctx, projectID)
	if err != nil {
		return nil, nil, err
	}
	var group db.PromotionGroup
	if err := s.db.WithContext(ctx).Where("id = ? AND project_id = ?", groupID, root.ID).First(&group).Error; err != nil {
		return nil, nil, err
	}
	return &group, root, nil
}

// RenameGroup renames a group. Renaming a single-service group makes it an
// ordinary one: someone has given it a name of its own.
func (s *PromotionService) RenameGroup(ctx context.Context, projectID, groupID uuid.UUID, name string) error {
	group, _, err := s.loadGroup(ctx, projectID, groupID)
	if err != nil {
		return err
	}
	return s.db.WithContext(ctx).Model(group).Updates(map[string]any{"name": name, "single": false}).Error
}

// AddToGroup adds services to a group, as creating it would have: each is
// copied into the level the group enters at, and stops building on push
// elsewhere on its path. A service in a single-service group is taken out of
// that one, which goes; a service in any other group is refused.
func (s *PromotionService) AddToGroup(ctx context.Context, projectID, groupID uuid.UUID, serviceIDs []uuid.UUID) error {
	group, root, err := s.loadGroup(ctx, projectID, groupID)
	if err != nil {
		return err
	}
	if len(serviceIDs) == 0 {
		return ErrGroupEmpty
	}
	levels, err := s.projects.Levels(ctx, root.ID)
	if err != nil {
		return err
	}
	chain := make([]uuid.UUID, len(levels))
	for i, l := range levels {
		chain[i] = l.ProjectID
	}
	var picked []db.Service
	if err := s.db.WithContext(ctx).Where("id IN ? AND project_id IN ?", serviceIDs, chain).Find(&picked).Error; err != nil {
		return err
	}
	if len(picked) != len(serviceIDs) {
		return gorm.ErrRecordNotFound
	}
	path := pathIDs(group.Path)
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var lineages []uuid.UUID
		for _, sv := range picked {
			if sv.Type == db.ServiceTypeDatabase {
				return ErrGroupDatabase
			}
			l := lineageOf(sv)
			var member db.PromotionGroupMember
			err := tx.Where("project_id = ? AND lineage_id = ?", root.ID, l).First(&member).Error
			switch {
			case err == nil && member.GroupID == group.ID:
				continue // already here
			case err == nil:
				var other db.PromotionGroup
				if err := tx.First(&other, "id = ?", member.GroupID).Error; err != nil {
					return err
				}
				if !other.Single {
					return ErrGroupMember
				}
				// Merged: the single-service group goes.
				if err := tx.Delete(&member).Error; err != nil {
					return err
				}
				if err := tx.Delete(&other).Error; err != nil {
					return err
				}
			case !errors.Is(err, gorm.ErrRecordNotFound):
				return err
			}
			if err := tx.Create(&db.PromotionGroupMember{GroupID: group.ID, ProjectID: root.ID, LineageID: l}).Error; err != nil {
				return err
			}
			lineages = append(lineages, l)
		}
		if len(lineages) == 0 {
			return nil
		}
		for _, l := range lineages {
			if _, err := s.ensureInLevel(ctx, tx, l, path[0], levels); err != nil {
				return err
			}
		}
		if err := tx.Model(group).Update("single", false).Error; err != nil {
			return err
		}
		return tx.Model(&db.BuildConfig{}).
			Where("service_id IN (?)", tx.Model(&db.Service{}).Select("id").
				Where("project_id IN ? AND project_id <> ? AND COALESCE(lineage_id, id) IN ?", path, path[0], lineages)).
			Update("auto_deploy", false).Error
	})
}

// RemoveFromGroup takes a service out of a group. Its copies stay in every
// level and keep running; it simply stops moving with the group. A group left
// empty goes. Reports whether it did.
func (s *PromotionService) RemoveFromGroup(ctx context.Context, projectID, groupID, lineageID uuid.UUID) (bool, error) {
	group, _, err := s.loadGroup(ctx, projectID, groupID)
	if err != nil {
		return false, err
	}
	deleted := false
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Where("group_id = ? AND lineage_id = ?", group.ID, lineageID).Delete(&db.PromotionGroupMember{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrNotInGroup
		}
		if err := releaseLineages(tx, pathIDs(group.Path), []uuid.UUID{lineageID}); err != nil {
			return err
		}
		var left int64
		if err := tx.Model(&db.PromotionGroupMember{}).Where("group_id = ?", group.ID).Count(&left).Error; err != nil {
			return err
		}
		if left == 0 {
			deleted = true
			return tx.Delete(group).Error
		}
		return nil
	})
	return deleted, err
}

// sameProject refuses a service and a level from different projects: a copy
// or bring-down stays inside one project's chain.
func (s *PromotionService) sameProject(ctx context.Context, a, b uuid.UUID) error {
	ra, err := s.projects.RootProject(ctx, a)
	if err != nil {
		return err
	}
	rb, err := s.projects.RootProject(ctx, b)
	if err != nil {
		return err
	}
	if ra.ID != rb.ID {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// levelOfProject is a project's position in its chain.
func (s *PromotionService) levelOfProject(ctx context.Context, id uuid.UUID) (int, error) {
	var p db.Project
	if err := s.db.WithContext(ctx).Select("env_level").First(&p, "id = ?", id).Error; err != nil {
		return 0, err
	}
	return p.EnvLevel, nil
}

// CopyToLevel copies one service into a lower level as a group of its own, so
// it can be promoted back up like anything else: everything in a level got
// there through a group. The group's path starts at that level and climbs
// through each level above that already has the service, to production. A
// service already in a group is refused; add it to that group's path instead.
func (s *PromotionService) CopyToLevel(ctx context.Context, serviceID, levelID uuid.UUID) (*db.PromotionGroup, error) {
	var sv db.Service
	if err := s.db.WithContext(ctx).First(&sv, "id = ?", serviceID).Error; err != nil {
		return nil, err
	}
	if err := s.sameProject(ctx, sv.ProjectID, levelID); err != nil {
		return nil, err
	}
	from, err := s.levelOfProject(ctx, sv.ProjectID)
	if err != nil {
		return nil, err
	}
	to, err := s.levelOfProject(ctx, levelID)
	if err != nil {
		return nil, err
	}
	if to <= from {
		return nil, ErrNotBelow
	}
	levels, err := s.projects.Levels(ctx, levelID)
	if err != nil {
		return nil, err
	}
	lineage := lineageOf(sv)
	var copies []db.Service
	if err := s.db.WithContext(ctx).Select("project_id").
		Where("COALESCE(lineage_id, id) = ?", lineage).Find(&copies).Error; err != nil {
		return nil, err
	}
	has := map[uuid.UUID]bool{}
	for _, c := range copies {
		has[c.ProjectID] = true
	}
	// levels runs production first; the path runs the other way.
	path := []uuid.UUID{levelID}
	for i := len(levels) - 1; i >= 0; i-- {
		l := levels[i]
		if l.Level < to && (has[l.ProjectID] || l.Production) {
			path = append(path, l.ProjectID)
		}
	}
	return s.CreateGroup(ctx, levelID, GroupInput{Name: sv.Name, ServiceIDs: []uuid.UUID{serviceID}, Path: path, Single: true})
}

// BringDown runs a service's current image in a lower level: production's exact
// build in staging, to reproduce a production bug there. The lower level gets a
// copy of the service first if it has none. It is not a rollback and moves
// nothing up; the lower level's next build replaces it as usual.
func (s *PromotionService) BringDown(ctx context.Context, serviceID, levelID uuid.UUID) (*db.Deployment, error) {
	var sv db.Service
	if err := s.db.WithContext(ctx).First(&sv, "id = ?", serviceID).Error; err != nil {
		return nil, err
	}
	if err := s.sameProject(ctx, sv.ProjectID, levelID); err != nil {
		return nil, err
	}
	from, err := s.levelOfProject(ctx, sv.ProjectID)
	if err != nil {
		return nil, err
	}
	to, err := s.levelOfProject(ctx, levelID)
	if err != nil {
		return nil, err
	}
	if to <= from {
		return nil, ErrNotBelow
	}
	var last db.Deployment
	if err := s.db.WithContext(ctx).
		Where("service_id = ? AND status = ? AND image <> ''", sv.ID, db.DeploymentSuccess).
		Order("created_at DESC").First(&last).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w: %s has never been deployed here", ErrNothingToPromote, sv.Name)
		}
		return nil, err
	}
	levels, err := s.projects.Levels(ctx, levelID)
	if err != nil {
		return nil, err
	}
	var fromName string
	for _, l := range levels {
		if l.ProjectID == sv.ProjectID {
			fromName = l.Name
		}
	}
	var target *db.Service
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		t, err := s.ensureInLevel(ctx, tx, lineageOf(sv), levelID, levels)
		target = t
		return err
	}); err != nil {
		return nil, err
	}
	return s.deployments.DeployImage(ctx, target.ID, last.Image,
		fmt.Sprintf("Brought down from %s (deployment %s, %s)", fromName, last.ID.String()[:8], last.Image),
		Provenance{Source: db.DeploySourceBringDown, FromLevel: fromName, From: &last})
}

// ErrRemoveProduction refuses removing production's copy through a level
// action: that is deleting the service, which its own page does.
var ErrRemoveProduction = errors.New("this is production's copy: delete the service from its own page instead")

// RemovedFromLevel reports what removing a copy took with it.
type RemovedFromLevel struct {
	RoutesRemoved int  `json:"routes_removed"`
	LeftGroup     bool `json:"left_group"`
	GroupDeleted  bool `json:"group_deleted"`
}

// RemoveFromLevel deletes one level's copy of a service: its workload, and the
// level's routes to it (a route to other targets as well keeps them and loses
// only this one). Production's copy is untouched, and the level uses the copy
// above from then on, like anything else it does not have.
//
// When the level was where the service's group builds, the service leaves the
// group too: it cannot be built there any more. A group left empty goes,
// which is always so for a single-service group.
func (s *PromotionService) RemoveFromLevel(ctx context.Context, serviceID uuid.UUID) (*RemovedFromLevel, error) {
	var sv db.Service
	if err := s.db.WithContext(ctx).First(&sv, "id = ?", serviceID).Error; err != nil {
		return nil, err
	}
	var level db.Project
	if err := s.db.WithContext(ctx).First(&level, "id = ?", sv.ProjectID).Error; err != nil {
		return nil, err
	}
	if level.ParentProjectID == nil {
		return nil, ErrRemoveProduction
	}
	out := &RemovedFromLevel{}
	lineage := lineageOf(sv)

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var routes []db.Route
		if err := tx.Preload("Targets").Where("project_id = ? AND id IN (?)", level.ID,
			tx.Model(&db.RouteTarget{}).Select("route_id").Where("service_id = ?", sv.ID)).Find(&routes).Error; err != nil {
			return err
		}
		for _, r := range routes {
			others := 0
			for _, t := range r.Targets {
				if t.ServiceID == nil || *t.ServiceID != sv.ID {
					others++
				}
			}
			if others == 0 {
				if err := tx.Delete(&db.Route{}, "id = ?", r.ID).Error; err != nil {
					return err
				}
				out.RoutesRemoved++
				continue
			}
			if err := tx.Where("route_id = ? AND service_id = ?", r.ID, sv.ID).Delete(&db.RouteTarget{}).Error; err != nil {
				return err
			}
		}

		var member db.PromotionGroupMember
		err := tx.Where("project_id = ? AND lineage_id = ?", *level.ParentProjectID, lineage).First(&member).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		var group db.PromotionGroup
		if err := tx.First(&group, "id = ?", member.GroupID).Error; err != nil {
			return err
		}
		path := pathIDs(group.Path)
		if len(path) == 0 || path[0] != level.ID {
			return nil // not where it builds: the next promotion brings it back
		}
		if err := tx.Delete(&member).Error; err != nil {
			return err
		}
		out.LeftGroup = true
		var left int64
		if err := tx.Model(&db.PromotionGroupMember{}).Where("group_id = ?", group.ID).Count(&left).Error; err != nil {
			return err
		}
		if left == 0 {
			out.GroupDeleted = true
			return tx.Delete(&group).Error
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	// The workload last, through the ordinary delete, which removes it from the
	// cluster before the row.
	if err := s.workloads.Delete(ctx, sv.ID); err != nil {
		return out, err
	}
	return out, nil
}

// ErrDeleteProduction refuses deleting production as a level: it is the
// project, deleted from the project's settings.
var ErrDeleteProduction = errors.New("production is the project itself: delete the project from its settings instead")

// DeletedLevel says what deleting a level changed besides the level itself.
type DeletedLevel struct {
	ServicesRemoved int `json:"services_removed"`
	// GroupsShortened lost this level from their path; GroupsDeleted had
	// nothing left to promote between once it was gone.
	GroupsShortened []string `json:"groups_shortened"`
	GroupsDeleted   []string `json:"groups_deleted"`
}

// DeleteLevel removes a level and everything in it. Its workloads leave the
// cluster first, through the ordinary delete, then its namespace with the
// volumes, jobs and secrets left in it. A group whose path passed through it
// skips it from then on; a group that built there builds at the next level
// on its path instead, which takes over the level's auto-deploy setting, and
// a group left with nothing between entry and production is dissolved.
func (s *PromotionService) DeleteLevel(ctx context.Context, levelID uuid.UUID) (*DeletedLevel, error) {
	var level db.Project
	if err := s.db.WithContext(ctx).First(&level, "id = ?", levelID).Error; err != nil {
		return nil, err
	}
	if level.ParentProjectID == nil {
		return nil, ErrDeleteProduction
	}
	rootID := *level.ParentProjectID
	out := &DeletedLevel{GroupsShortened: []string{}, GroupsDeleted: []string{}}

	var services []db.Service
	if err := s.db.WithContext(ctx).Where("project_id = ?", level.ID).Find(&services).Error; err != nil {
		return nil, err
	}
	// Whether each lineage built on push here, for the level that takes over.
	autoDeploy := map[uuid.UUID]bool{}
	for _, sv := range services {
		var bc db.BuildConfig
		if s.db.WithContext(ctx).First(&bc, "service_id = ?", sv.ID).Error == nil {
			autoDeploy[lineageOf(sv)] = bc.AutoDeploy
		}
	}
	n, err := s.teardown(ctx, level)
	out.ServicesRemoved = n
	if err != nil {
		return out, err
	}

	levels, err := s.projects.Levels(ctx, rootID)
	if err != nil {
		return out, err
	}
	remaining := make([]EnvironmentLevel, 0, len(levels))
	for _, l := range levels {
		if l.ProjectID != level.ID {
			remaining = append(remaining, l)
		}
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var groups []db.PromotionGroup
		if err := tx.Where("project_id = ?", rootID).Find(&groups).Error; err != nil {
			return err
		}
		for _, g := range groups {
			path := pathIDs(g.Path)
			at := slices.Index(path, level.ID)
			if at < 0 {
				continue
			}
			path = slices.Delete(path, at, at+1)
			if at == 0 {
				var lineages []uuid.UUID
				if err := tx.Model(&db.PromotionGroupMember{}).Where("group_id = ?", g.ID).
					Pluck("lineage_id", &lineages).Error; err != nil {
					return err
				}
				for _, l := range lineages {
					sv, err := s.ensureInLevel(ctx, tx, l, path[0], remaining)
					if err != nil {
						return err
					}
					if err := tx.Model(&db.BuildConfig{}).Where("service_id = ?", sv.ID).
						Update("auto_deploy", autoDeploy[l]).Error; err != nil {
						return err
					}
				}
			}
			if len(path) < 2 {
				if err := tx.Where("group_id = ?", g.ID).Delete(&db.PromotionGroupMember{}).Error; err != nil {
					return err
				}
				if err := tx.Delete(&g).Error; err != nil {
					return err
				}
				out.GroupsDeleted = append(out.GroupsDeleted, g.Name)
				continue
			}
			if err := tx.Model(&g).Update("path", pathStrings(path)).Error; err != nil {
				return err
			}
			out.GroupsShortened = append(out.GroupsShortened, g.Name)
		}
		tx.Where("resource_type = ? AND resource_id = ?", db.ResourceProject, level.ID).Delete(&db.ResourcePermission{})
		if err := tx.Delete(&db.Project{}, "id = ?", level.ID).Error; err != nil {
			return err
		}
		return closeGap(tx, rootID, level.EnvLevel)
	})
	if err != nil {
		return out, err
	}
	return out, nil
}

// fromBelowDetail is ErrBorrowFromBelow's message without its prefix.
func fromBelowDetail(err error) string {
	return strings.TrimPrefix(err.Error(), ErrBorrowFromBelow.Error()+": ")
}

// PreflightNote is what the Promote dialog says about one service before it
// moves: whether it is new to the target, how many variables of its own it
// brings there, and what would stop it.
type PreflightNote struct {
	ServiceName string `json:"service_name"`
	// NewThere is set when the target has no copy yet: promoting creates one,
	// with the source's own variables as they are.
	NewThere     bool `json:"new_there"`
	OwnVariables int  `json:"own_variables"`
	// Blocked says what the target needs first, when its variables would
	// come from below it.
	Blocked string `json:"blocked,omitempty"`
}

// Preflight notes each of the group's services in level before promoting it
// to the next level on the group's path.
func (s *PromotionService) Preflight(ctx context.Context, level, groupID uuid.UUID) ([]PreflightNote, error) {
	next, err := s.NextLevel(ctx, level, groupID)
	if err != nil {
		return nil, err
	}
	var members []db.PromotionGroupMember
	if err := s.db.WithContext(ctx).Where("group_id = ?", groupID).Find(&members).Error; err != nil {
		return nil, err
	}
	out := []PreflightNote{}
	for _, m := range members {
		var src db.Service
		if err := s.db.WithContext(ctx).Where("project_id = ? AND COALESCE(lineage_id, id) = ?", level, m.LineageID).First(&src).Error; err != nil {
			continue // not in this level: Promote skips it, and says so
		}
		note := PreflightNote{ServiceName: src.Name}
		attached := src.ID
		var up db.Service
		if s.db.WithContext(ctx).Where("project_id = ? AND COALESCE(lineage_id, id) = ?", next, m.LineageID).First(&up).Error == nil {
			attached = up.ID
		} else {
			note.NewThere = true
			note.OwnVariables = len(appk8s.ParseEnvBlock(string(src.EnvVars)))
		}
		if err := s.deployments.varGroups.CheckBorrowingAt(ctx, next, attached); err != nil {
			if !errors.Is(err, ErrBorrowFromBelow) {
				return nil, err
			}
			note.Blocked = fromBelowDetail(err)
		}
		out = append(out, note)
	}
	return out, nil
}

// releaseLineages hands services leaving a group back their own builds. The
// group turned auto-deploy off above its entry level, where copies received
// promotions instead; with no group, nothing promotes to them, so each takes
// the entry's setting back and deploys on push again if the entry did.
func releaseLineages(tx *gorm.DB, path, lineages []uuid.UUID) error {
	if len(path) < 2 {
		return nil
	}
	for _, l := range lineages {
		var entry db.BuildConfig
		err := tx.Where("service_id IN (?)", tx.Model(&db.Service{}).Select("id").
			Where("project_id = ? AND COALESCE(lineage_id, id) = ?", path[0], l)).First(&entry).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			continue // nothing built at the entry: nothing to hand back
		}
		if err != nil {
			return err
		}
		if err := tx.Model(&db.BuildConfig{}).
			Where("service_id IN (?)", tx.Model(&db.Service{}).Select("id").
				Where("project_id IN ? AND COALESCE(lineage_id, id) = ?", path[1:], l)).
			Update("auto_deploy", entry.AutoDeploy).Error; err != nil {
			return err
		}
	}
	return nil
}

// teardown takes a project's or a level's workloads out of the cluster: each
// service through the ordinary delete, which removes its cluster objects
// before its row, then the namespace with the volumes, jobs and secrets left
// in it. The rows that remain go with the project's row, by cascade.
func (s *PromotionService) teardown(ctx context.Context, project db.Project) (int, error) {
	var services []db.Service
	if err := s.db.WithContext(ctx).Where("project_id = ?", project.ID).Find(&services).Error; err != nil {
		return 0, err
	}
	removed := 0
	for _, sv := range services {
		if err := s.workloads.Delete(ctx, sv.ID); err != nil {
			return removed, err
		}
		removed++
	}
	if s.workloads.k8s != nil {
		if err := appk8s.DeleteNamespace(ctx, s.workloads.k8s, project.Slug); err != nil {
			return removed, err
		}
	}
	return removed, nil
}

// DeleteProject deletes a project and, first, everything it runs, so nothing
// is left in the cluster under a namespace no project owns. A project with
// levels is refused before anything is touched; a level is deleted with
// DeleteLevel.
func (s *PromotionService) DeleteProject(ctx context.Context, projectID uuid.UUID) error {
	var project db.Project
	if err := s.db.WithContext(ctx).First(&project, "id = ?", projectID).Error; err != nil {
		return err
	}
	if project.ParentProjectID != nil {
		_, err := s.DeleteLevel(ctx, projectID)
		return err
	}
	var levels int64
	if err := s.db.WithContext(ctx).Model(&db.Project{}).Where("parent_project_id = ?", projectID).Count(&levels).Error; err != nil {
		return err
	}
	if levels > 0 {
		return ErrProjectHasLevels
	}
	if _, err := s.teardown(ctx, project); err != nil {
		return err
	}
	return s.projects.Delete(ctx, projectID)
}

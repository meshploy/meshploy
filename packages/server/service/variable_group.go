package service

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type VariableGroupService struct {
	db *gorm.DB
}

// ─── Read helpers ─────────────────────────────────────────────────────────────

func (s *VariableGroupService) List(ctx context.Context, projectID uuid.UUID) ([]db.VariableGroup, error) {
	var groups []db.VariableGroup
	err := s.db.WithContext(ctx).
		Preload("Items").
		Where("project_id = ?", projectID).
		Order("system_managed ASC, created_at ASC").
		Find(&groups).Error
	return groups, err
}

func (s *VariableGroupService) Get(ctx context.Context, groupID, projectID uuid.UUID) (*db.VariableGroup, error) {
	var g db.VariableGroup
	err := s.db.WithContext(ctx).Preload("Items").
		First(&g, "id = ? AND project_id = ?", groupID, projectID).Error
	return &g, err
}

func (s *VariableGroupService) getByID(ctx context.Context, groupID uuid.UUID) (*db.VariableGroup, error) {
	var g db.VariableGroup
	err := s.db.WithContext(ctx).Preload("Items").First(&g, "id = ?", groupID).Error
	return &g, err
}

// ListForService returns all groups attached to a service (items included).
func (s *VariableGroupService) ListForService(ctx context.Context, serviceID uuid.UUID) ([]db.VariableGroup, error) {
	var svgs []db.ServiceVariableGroup
	if err := s.db.WithContext(ctx).Where("service_id = ?", serviceID).Find(&svgs).Error; err != nil {
		return nil, err
	}
	if len(svgs) == 0 {
		return nil, nil
	}
	ids := make([]uuid.UUID, len(svgs))
	for i, sv := range svgs {
		ids[i] = sv.GroupID
	}
	var groups []db.VariableGroup
	err := s.db.WithContext(ctx).Preload("Items").Where("id IN ?", ids).Find(&groups).Error
	return groups, err
}

// ListForJob returns the groups attached to a job. A job has no system-managed
// group of its own -- it owns no database -- so every group here was attached
// deliberately.
func (s *VariableGroupService) ListForJob(ctx context.Context, jobID uuid.UUID) ([]db.VariableGroup, error) {
	var jvgs []db.JobVariableGroup
	if err := s.db.WithContext(ctx).Where("job_id = ?", jobID).Find(&jvgs).Error; err != nil {
		return nil, err
	}
	if len(jvgs) == 0 {
		return nil, nil
	}
	ids := make([]uuid.UUID, len(jvgs))
	for i, jv := range jvgs {
		ids[i] = jv.GroupID
	}
	var groups []db.VariableGroup
	err := s.db.WithContext(ctx).Preload("Items").Where("id IN ?", ids).Find(&groups).Error
	return groups, err
}

// ─── User-managed group CRUD ──────────────────────────────────────────────────

type CreateGroupInput struct {
	ProjectID   uuid.UUID
	Name        string
	Description string
}

func (s *VariableGroupService) Create(ctx context.Context, in CreateGroupInput) (*db.VariableGroup, error) {
	g := db.VariableGroup{
		ProjectID:   in.ProjectID,
		Name:        in.Name,
		Description: in.Description,
	}
	if err := s.db.WithContext(ctx).Create(&g).Error; err != nil {
		return nil, err
	}
	return &g, nil
}

type UpdateGroupInput struct {
	Name        *string
	Description *string
}

func (s *VariableGroupService) Update(ctx context.Context, groupID uuid.UUID, in UpdateGroupInput) (*db.VariableGroup, error) {
	updates := map[string]any{}
	if in.Name != nil {
		updates["name"] = *in.Name
	}
	if in.Description != nil {
		updates["description"] = *in.Description
	}
	if len(updates) > 0 {
		if err := s.db.WithContext(ctx).Model(&db.VariableGroup{}).
			Where("id = ? AND system_managed = false", groupID).
			Updates(updates).Error; err != nil {
			return nil, err
		}
	}
	return s.getByID(ctx, groupID)
}

func (s *VariableGroupService) Delete(ctx context.Context, groupID uuid.UUID) error {
	return s.db.WithContext(ctx).
		Where("id = ? AND system_managed = false", groupID).
		Delete(&db.VariableGroup{}).Error
}

// ─── Item CRUD ────────────────────────────────────────────────────────────────

type UpsertItemInput struct {
	Key      string
	Value    string
	IsSecret bool
}

func (s *VariableGroupService) UpsertItem(ctx context.Context, groupID uuid.UUID, in UpsertItemInput) (*db.VariableGroupItem, error) {
	item := db.VariableGroupItem{
		GroupID:  groupID,
		Key:      in.Key,
		Value:    db.EncryptedString(in.Value),
		IsSecret: in.IsSecret,
	}
	err := s.db.WithContext(ctx).
		Where(db.VariableGroupItem{GroupID: groupID, Key: in.Key}).
		Assign(db.VariableGroupItem{Value: db.EncryptedString(in.Value), IsSecret: in.IsSecret}).
		FirstOrCreate(&item).Error
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (s *VariableGroupService) DeleteItem(ctx context.Context, itemID uuid.UUID) error {
	return s.db.WithContext(ctx).Delete(&db.VariableGroupItem{}, "id = ?", itemID).Error
}

// ─── Service attachment ───────────────────────────────────────────────────────

func (s *VariableGroupService) Attach(ctx context.Context, serviceID, groupID uuid.UUID) error {
	svg := db.ServiceVariableGroup{ServiceID: serviceID, GroupID: groupID}
	return s.db.WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		Create(&svg).Error
}

func (s *VariableGroupService) Detach(ctx context.Context, serviceID, groupID uuid.UUID) error {
	// Block detaching a system-managed group from the service that owns it.
	var g db.VariableGroup
	if err := s.db.WithContext(ctx).Select("system_managed, service_id").First(&g, "id = ?", groupID).Error; err != nil {
		return err
	}
	if g.SystemManaged && g.ServiceID != nil && *g.ServiceID == serviceID {
		return fmt.Errorf("cannot detach a service's own system-managed variable group")
	}
	return s.db.WithContext(ctx).
		Where("service_id = ? AND group_id = ?", serviceID, groupID).
		Delete(&db.ServiceVariableGroup{}).Error
}

// ─── System-managed group helpers ─────────────────────────────────────────────

var nonAlphanumRe = regexp.MustCompile(`[^A-Z0-9]+`)

// serviceDNSName is the in-cluster address of a service.
//
// It is built from the K8s Service name — the slugified service name — not the
// service name itself. A name carrying a space or a capital ("My DB") is not a
// legal DNS label, and a database's workload is deployed under a suffixed slug,
// so addressing either by its raw name produced a hostname that does not exist.
// That does not fail loudly: an unmatched name falls through to the mesh search
// domain and resolves to the gateway, so the caller connects to the wrong host
// and looks healthy. Databases are published under this name as an alias
// Service, so one canonical form covers applications and databases alike.
func serviceDNSName(serviceName, namespace string) string {
	return fmt.Sprintf("%s.%s.svc.cluster.local", slugify(serviceName), namespace)
}

// serviceEnvPrefix converts a service name to an env var prefix.
// "auth-api" → "AUTH_API", "my.service" → "MY_SERVICE"
func serviceEnvPrefix(name string) string {
	upper := strings.ToUpper(name)
	return nonAlphanumRe.ReplaceAllString(upper, "_")
}

// UpsertSystemGroup creates or fully replaces the system-managed variable group
// for a service. Called at service creation and after each successful deploy.
func (s *VariableGroupService) UpsertSystemGroup(ctx context.Context, svc *db.Service, namespace string) error {
	prefix := serviceEnvPrefix(svc.Name)
	host := serviceDNSName(svc.Name, namespace)

	// Find or create the group
	var group db.VariableGroup
	err := s.db.WithContext(ctx).
		Where("service_id = ?", svc.ID).
		FirstOrCreate(&group, db.VariableGroup{
			ProjectID:     svc.ProjectID,
			ServiceID:     &svc.ID,
			Name:          svc.Name + " (service)",
			Description:   "Auto-generated service discovery variables",
			SystemManaged: true,
		}).Error
	if err != nil {
		return err
	}

	// Build all items for this service's ports, and for a database what an app
	// needs to connect to it. The database's own items replace any the ports
	// produced under the same key.
	ports := svc.Ports
	if len(ports) == 0 {
		if err := s.db.WithContext(ctx).Where("service_id = ?", svc.ID).Find(&ports).Error; err != nil {
			return err
		}
	}
	items := buildServiceItems(prefix, host, ports)
	if svc.Type == db.ServiceTypeDatabase {
		var dc db.DatabaseConfig
		if err := s.db.WithContext(ctx).Where("service_id = ?", svc.ID).First(&dc).Error; err == nil {
			items = mergeItems(items, buildDatabaseItems(prefix, host, primaryPort(ports), dc))
		}
	}

	// Replace all items atomically.
	//
	// The group is deliberately NOT attached to the service it describes. These
	// variables exist so OTHER services can reach this one; a service already
	// knows its own address, and `runtimeEnvVars` injects its PORT separately.
	//
	// Injecting them into the service itself actively breaks applications. The
	// key is derived from the service's own name, so an application whose own
	// configuration variable follows the same pattern gets it silently
	// overwritten with a value meaning something else. Uptime Kuma reads
	// `UPTIME_KUMA_HOST` as the address to BIND; a service named `uptime-kuma`
	// was handed its own cluster FQDN there and crash-looped on EADDRNOTAVAIL,
	// because a pod cannot bind a Service address. Self-attachment is also the
	// only case where that collision is systematic rather than coincidental.
	//
	// Any prior self-attachment is removed, so existing services are repaired on
	// their next deploy rather than staying broken until recreated.
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("group_id = ?", group.ID).Delete(&db.VariableGroupItem{}).Error; err != nil {
			return err
		}
		for _, item := range items {
			item.GroupID = group.ID
			if err := tx.Create(&item).Error; err != nil {
				return err
			}
		}
		return tx.Where("service_id = ? AND group_id = ?", svc.ID, group.ID).
			Delete(&db.ServiceVariableGroup{}).Error
	})
}

func buildServiceItems(prefix, host string, ports []db.ServicePort) []db.VariableGroupItem {
	var items []db.VariableGroupItem

	add := func(key, value string) {
		items = append(items, db.VariableGroupItem{
			Key:      key,
			Value:    db.EncryptedString(value),
			IsSecret: false,
		})
	}

	for _, p := range ports {
		addr := fmt.Sprintf("%s:%d", host, p.Port)
		if p.IsPrimary {
			// Primary port — no name suffix
			add(prefix+"_HOST", host)
			add(prefix+"_PORT", fmt.Sprintf("%d", p.Port))
			if p.IsHTTP {
				add(prefix+"_URL", "http://"+addr)
			} else {
				add(prefix+"_ADDR", addr)
			}
		} else {
			// Named non-primary port — suffix with uppercased port name
			portPrefix := prefix + "_" + nonAlphanumRe.ReplaceAllString(strings.ToUpper(p.Name), "_")
			add(portPrefix+"_PORT", fmt.Sprintf("%d", p.Port))
			if p.IsHTTP {
				add(portPrefix+"_URL", "http://"+addr)
			} else {
				add(portPrefix+"_ADDR", addr)
			}
		}
	}
	return items
}

// buildDatabaseItems publishes what an app needs to connect to a managed
// database: its user, password and name, and a connection URL in the engine's
// own scheme. The password, and the URL that carries it, are secret.
func buildDatabaseItems(prefix, host string, port int32, dc db.DatabaseConfig) []db.VariableGroupItem {
	item := func(key, value string, secret bool) db.VariableGroupItem {
		return db.VariableGroupItem{Key: key, Value: db.EncryptedString(value), IsSecret: secret}
	}
	pass := string(dc.DBPassword)
	var items []db.VariableGroupItem
	if dc.Engine != db.DatabaseRedis && dc.Engine != db.DatabaseDragonfly {
		items = append(items, item(prefix+"_USER", dc.DBUser, false), item(prefix+"_DB", dc.DBName, false))
	}
	if pass != "" {
		items = append(items, item(prefix+"_PASSWORD", pass, true))
	}
	if u := databaseURL(dc.Engine, dc.DBUser, pass, host, port, dc.DBName); u != "" {
		items = append(items, item(prefix+"_URL", u, pass != ""))
	}
	return items
}

// databaseURL is the connection URL for an engine, with the user and password
// percent-encoded. MongoDB's root user lives in the admin database, and
// ClickHouse is reached on its native port, the one Meshploy exposes.
func databaseURL(engine db.DatabaseEngine, user, pass, host string, port int32, name string) string {
	u := url.URL{Host: fmt.Sprintf("%s:%d", host, port)}
	switch engine {
	case db.DatabasePostgres:
		u.Scheme = "postgresql"
	case db.DatabaseMySQL:
		u.Scheme = "mysql"
	case db.DatabaseMongoDB:
		u.Scheme, u.RawQuery = "mongodb", "authSource=admin"
	case db.DatabaseClickHouse:
		u.Scheme = "clickhouse"
	case db.DatabaseRedis, db.DatabaseDragonfly:
		u.Scheme = "redis"
		if pass != "" {
			u.User = url.UserPassword("", pass)
		}
		return u.String()
	default:
		return ""
	}
	u.User = url.UserPassword(user, pass)
	u.Path = "/" + name
	return u.String()
}

// mergeItems appends extra to base, an item in extra replacing one in base
// under the same key.
func mergeItems(base, extra []db.VariableGroupItem) []db.VariableGroupItem {
	replaced := make(map[string]bool, len(extra))
	for _, it := range extra {
		replaced[it.Key] = true
	}
	out := make([]db.VariableGroupItem, 0, len(base)+len(extra))
	for _, it := range base {
		if !replaced[it.Key] {
			out = append(out, it)
		}
	}
	return append(out, extra...)
}

// CollectEnvVars loads all variable groups attached to a service and returns
// them as a flat key→value map, ready for injection into the K8s Deployment.
// Items from later-attached groups win on key conflict.
// AttachJob and DetachJob mirror Attach and Detach. Nothing is redeployed: a
// job has nothing running, so the next run reads the new values.
func (s *VariableGroupService) AttachJob(ctx context.Context, jobID, groupID uuid.UUID) error {
	jvg := db.JobVariableGroup{JobID: jobID, GroupID: groupID}
	return s.db.WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		Create(&jvg).Error
}

func (s *VariableGroupService) DetachJob(ctx context.Context, jobID, groupID uuid.UUID) error {
	return s.db.WithContext(ctx).
		Where("job_id = ? AND group_id = ?", jobID, groupID).
		Delete(&db.JobVariableGroup{}).Error
}

// CollectEnvVarsForJob is CollectEnvVars for a job's attachments.
func (s *VariableGroupService) CollectEnvVarsForJob(ctx context.Context, jobID uuid.UUID) (map[string]string, error) {
	groups, err := s.ListForJob(ctx, jobID)
	if err != nil {
		return nil, err
	}
	var job db.Job
	if err := s.db.WithContext(ctx).Select("project_id").First(&job, "id = ?", jobID).Error; err != nil {
		return nil, err
	}
	groups, err = s.nearestCopies(ctx, job.ProjectID, groups)
	if err != nil {
		return nil, err
	}
	return flattenGroups(groups), nil
}

func (s *VariableGroupService) CollectEnvVars(ctx context.Context, serviceID uuid.UUID) (map[string]string, error) {
	groups, err := s.ListForService(ctx, serviceID)
	if err != nil {
		return nil, err
	}
	var svc db.Service
	if err := s.db.WithContext(ctx).Select("project_id").First(&svc, "id = ?", serviceID).Error; err != nil {
		return nil, err
	}
	groups, err = s.nearestCopies(ctx, svc.ProjectID, groups)
	if err != nil {
		return nil, err
	}
	return flattenGroups(groups), nil
}

// nearestCopies is how an environment level borrows what it does not have.
//
// A service reaches another through the other's published group (its host,
// port, URL, and for a database how to connect), and those values name the
// publisher's namespace. Across levels a service is several copies, one per
// level, so each published group attached here is swapped for the one
// published by the copy in the nearest level at or above the consumer's own
// level: staging's web uses staging's database when staging has one, and the
// one above when it does not. Only copies that have been deployed publish a
// group, so a copy that has never run is passed over for the level above.
//
// Resolved at every deploy rather than when groups are attached, so giving a
// level its own database later is enough for its services to use it on their
// next deploy. Groups that are not published by a service are left alone.
//
// A level never uses anything from below it: that would be production reading
// staging's values, or connecting to staging's database. A shared group that
// belongs to a lower level, or a published one whose service has no running
// copy at or above this level, is refused with ErrBorrowFromBelow, which names
// what this level needs of its own.
func (s *VariableGroupService) nearestCopies(ctx context.Context, projectID uuid.UUID, groups []db.VariableGroup) ([]db.VariableGroup, error) {
	if len(groups) == 0 {
		return groups, nil
	}
	var publishers []uuid.UUID
	for _, g := range groups {
		if g.ServiceID != nil {
			publishers = append(publishers, *g.ServiceID)
		}
	}
	var here db.Project
	if err := s.db.WithContext(ctx).First(&here, "id = ?", projectID).Error; err != nil {
		return nil, err
	}
	root := here.ID
	if here.ParentProjectID != nil {
		root = *here.ParentProjectID
	}
	var chain []db.Project
	if err := s.db.WithContext(ctx).Select("id", "env_level", "env_name", "parent_project_id").
		Where("id = ? OR parent_project_id = ?", root, root).Find(&chain).Error; err != nil {
		return nil, err
	}
	if len(chain) < 2 {
		return groups, nil // a project with no levels has nothing to borrow
	}
	levelOf := make(map[uuid.UUID]int, len(chain))
	nameOf := make(map[uuid.UUID]string, len(chain))
	chainIDs := make([]uuid.UUID, len(chain))
	for i, p := range chain {
		levelOf[p.ID] = p.EnvLevel
		nameOf[p.ID] = p.EnvName
		if p.ParentProjectID == nil {
			nameOf[p.ID] = "production"
		}
		chainIDs[i] = p.ID
	}
	mine := here.EnvLevel
	for _, g := range groups {
		if lvl, ok := levelOf[g.ProjectID]; ok && g.ServiceID == nil && lvl > mine {
			return nil, fmt.Errorf("%w: the %s variable group belongs to %s, below %s; give %s a group of its own instead",
				ErrBorrowFromBelow, g.Name, nameOf[g.ProjectID], nameOf[here.ID], nameOf[here.ID])
		}
	}
	if len(publishers) == 0 {
		return groups, nil
	}

	var pubs []db.Service
	if err := s.db.WithContext(ctx).Select("id", "lineage_id", "name", "project_id").Where("id IN ?", publishers).Find(&pubs).Error; err != nil {
		return nil, err
	}
	pubByID := make(map[uuid.UUID]db.Service, len(pubs))
	for _, p := range pubs {
		pubByID[p.ID] = p
	}
	lineageOfPub := make(map[uuid.UUID]uuid.UUID, len(pubs))
	lineages := make([]uuid.UUID, 0, len(pubs))
	for _, p := range pubs {
		l := lineageOf(p)
		lineageOfPub[p.ID] = l
		lineages = append(lineages, l)
	}

	// Every copy of those services in this chain that publishes a group.
	var copies []db.Service
	if err := s.db.WithContext(ctx).Select("id", "project_id", "lineage_id").
		Where("project_id IN ? AND COALESCE(lineage_id, id) IN ?", chainIDs, lineages).Find(&copies).Error; err != nil {
		return nil, err
	}
	copyIDs := make([]uuid.UUID, len(copies))
	for i, c := range copies {
		copyIDs[i] = c.ID
	}
	var published []db.VariableGroup
	if err := s.db.WithContext(ctx).Preload("Items").
		Where("service_id IN ? AND system_managed = ?", copyIDs, true).Find(&published).Error; err != nil {
		return nil, err
	}
	groupOf := make(map[uuid.UUID]db.VariableGroup, len(published))
	for _, g := range published {
		groupOf[*g.ServiceID] = g
	}

	out := make([]db.VariableGroup, len(groups))
	for i, g := range groups {
		out[i] = g
		if g.ServiceID == nil {
			continue
		}
		lineage := lineageOfPub[*g.ServiceID]
		best, bestLevel := db.VariableGroup{}, -1
		for _, c := range copies {
			lvl, ok := levelOf[c.ProjectID]
			pg, published := groupOf[c.ID]
			if !ok || !published || lineageOf(c) != lineage || lvl > mine {
				continue // not this service, not published, or below the consumer
			}
			if lvl > bestLevel {
				best, bestLevel = pg, lvl
			}
		}
		if bestLevel >= 0 {
			out[i] = best
			continue
		}
		// Nothing at or above: the group attached is all there is. Used as it
		// is only when it is not from below.
		if pub, ok := pubByID[*g.ServiceID]; ok {
			if lvl, inChain := levelOf[pub.ProjectID]; inChain && lvl > mine {
				return nil, fmt.Errorf("%w: it uses %s's variables, and the only running %s is in %s, below %s; give %s its own %s first",
					ErrBorrowFromBelow, pub.Name, pub.Name, nameOf[pub.ProjectID], nameOf[here.ID], nameOf[here.ID], pub.Name)
			}
		}
	}
	return out, nil
}

// ErrBorrowFromBelow refuses variables a level would take from a level below
// it; the message says what the level needs of its own.
var ErrBorrowFromBelow = errors.New("a level cannot use variables from a level below it")

// CheckBorrowing is nil when serviceID's variables can be resolved without
// reaching below its level, and ErrBorrowFromBelow saying why not otherwise.
func (s *VariableGroupService) CheckBorrowing(ctx context.Context, serviceID uuid.UUID) error {
	_, err := s.CollectEnvVars(ctx, serviceID)
	return err
}

// CheckBorrowingAt is CheckBorrowing for serviceID's variable groups as they
// would be at another level, for a service about to be copied there.
func (s *VariableGroupService) CheckBorrowingAt(ctx context.Context, levelID, serviceID uuid.UUID) error {
	groups, err := s.ListForService(ctx, serviceID)
	if err != nil {
		return err
	}
	_, err = s.nearestCopies(ctx, levelID, groups)
	return err
}

func flattenGroups(groups []db.VariableGroup) map[string]string {
	out := make(map[string]string)
	for _, g := range groups {
		for _, item := range g.Items {
			out[item.Key] = string(item.Value)
		}
	}
	return out
}

// Dependent is a service or job whose variables come, today, from the group
// another service publishes: what stops working when that service goes.
type Dependent struct {
	Kind  string `json:"kind"` // "service" or "job"
	Name  string `json:"name"`
	Level string `json:"level"` // the environment level it is in; "production" for a project's own
}

// Dependents lists who reads serviceID's published variables now, at any
// level. A consumer attached to another copy's group counts when that group
// resolves to this one (a staging service using production's database); one
// whose own level has a copy of its own does not. Deleting serviceID removes
// its group, and each of these loses those variables on its next deploy.
func (s *VariableGroupService) Dependents(ctx context.Context, serviceID uuid.UUID) ([]Dependent, error) {
	out := []Dependent{}
	var svc db.Service
	if err := s.db.WithContext(ctx).First(&svc, "id = ?", serviceID).Error; err != nil {
		return nil, err
	}
	var own db.VariableGroup
	if err := s.db.WithContext(ctx).Where("service_id = ? AND system_managed = ?", serviceID, true).First(&own).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return out, nil // publishes nothing, so nothing reads it
		}
		return nil, err
	}
	lineage := lineageOf(svc)
	// Every group any copy of it publishes: consumers attach to their own
	// level's copy, or to one above.
	var groupIDs []uuid.UUID
	if err := s.db.WithContext(ctx).Model(&db.VariableGroup{}).
		Where("system_managed = ? AND service_id IN (?)", true,
			s.db.Model(&db.Service{}).Select("id").Where("COALESCE(lineage_id, id) = ?", lineage)).
		Pluck("id", &groupIDs).Error; err != nil {
		return nil, err
	}
	levelName := func(projectID uuid.UUID) string {
		var p db.Project
		if s.db.WithContext(ctx).Select("env_name", "parent_project_id").First(&p, "id = ?", projectID).Error != nil || p.ParentProjectID == nil {
			return "production"
		}
		return p.EnvName
	}
	resolvesHere := func(projectID uuid.UUID, groups []db.VariableGroup) bool {
		resolved, err := s.nearestCopies(ctx, projectID, groups)
		if err != nil {
			return false
		}
		for _, g := range resolved {
			if g.ID == own.ID {
				return true
			}
		}
		return false
	}

	var consumers []db.Service
	if err := s.db.WithContext(ctx).Where("id IN (?) AND COALESCE(lineage_id, id) <> ?",
		s.db.Model(&db.ServiceVariableGroup{}).Select("service_id").Where("group_id IN ?", groupIDs), lineage).
		Order("name").Find(&consumers).Error; err != nil {
		return nil, err
	}
	for _, c := range consumers {
		groups, err := s.ListForService(ctx, c.ID)
		if err != nil {
			return nil, err
		}
		if resolvesHere(c.ProjectID, groups) {
			out = append(out, Dependent{Kind: "service", Name: c.Name, Level: levelName(c.ProjectID)})
		}
	}
	var jobs []db.Job
	if err := s.db.WithContext(ctx).Where("id IN (?)",
		s.db.Model(&db.JobVariableGroup{}).Select("job_id").Where("group_id IN ?", groupIDs)).
		Order("name").Find(&jobs).Error; err != nil {
		return nil, err
	}
	for _, j := range jobs {
		groups, err := s.ListForJob(ctx, j.ID)
		if err != nil {
			return nil, err
		}
		if resolvesHere(j.ProjectID, groups) {
			out = append(out, Dependent{Kind: "job", Name: j.Name, Level: levelName(j.ProjectID)})
		}
	}
	return out, nil
}

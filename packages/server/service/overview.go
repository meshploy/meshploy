package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
	"gorm.io/gorm"
)

// The workspace overview's two questions beyond "what exists": does anything
// need me, and what is about to change.
//
// Every item here is read from state the platform already keeps: a failed
// service, an offline node, a level ahead of the one above it. Nothing is an
// event invented for the list, so an item goes away by itself when the thing
// it names is dealt with.

// OverviewService assembles the workspace overview.
type OverviewService struct {
	db         *gorm.DB
	projects   *ProjectService
	promotions *PromotionService
	orphans    *OrphanService
	workloads  *WorkloadService
	// metrics reads a node's live metrics (node_exporter); replaceable in tests.
	metrics func(ctx context.Context, nodeID uuid.UUID) (*NodeMetrics, error)

	diskMu    sync.Mutex
	diskCache map[uuid.UUID]diskReading
}

// diskReading is a node's disk as last read, kept for diskReadingTTL so the
// overview's refresh does not scrape every node every few seconds.
type diskReading struct {
	total, avail int64
	at           time.Time
}

const (
	diskReadingTTL = time.Minute
	// A node's disk is worth a look from diskWarnPercent full, and an emergency
	// from diskCriticalPercent: builds and databases fail when it fills.
	diskWarnPercent     = 85
	diskCriticalPercent = 95
)

// Attention severities, most urgent first.
const (
	SeverityCritical = "critical" // something is down
	SeverityWarning  = "warning"  // something will hurt later: no backups, a failed run
	SeverityInfo     = "info"     // something is waiting on a person: a promotion
)

// AttentionItem is one thing that needs someone. Kind says what it is and so
// where the console links it; the ids fill that link.
type AttentionItem struct {
	Kind     string `json:"kind"`
	Severity string `json:"severity"`
	Title    string `json:"title"`
	Detail   string `json:"detail,omitempty"`

	ProjectID    *uuid.UUID `json:"project_id,omitempty"`
	ServiceID    *uuid.UUID `json:"service_id,omitempty"`
	DeploymentID *uuid.UUID `json:"deployment_id,omitempty"`
	JobID        *uuid.UUID `json:"job_id,omitempty"`
	NodeID       *uuid.UUID `json:"node_id,omitempty"`
	DomainID     *uuid.UUID `json:"domain_id,omitempty"`
}

// Overview is the attention list and the workspace-wide breakdowns.
type Overview struct {
	Attention []AttentionItem           `json:"attention"`
	Stats     map[string]map[string]int `json:"stats"`
	Delivery  Delivery                  `json:"delivery"`
}

// DeliveryDays is how far back Delivery looks.
const DeliveryDays = 14

// Delivery is how the workspace ships, over the last DeliveryDays days: what
// ran each day, and the four numbers teams judge delivery by. A number with
// nothing to measure it from is nil, never a zero that reads as a result.
type Delivery struct {
	Days []DeliveryDay `json:"days"`
	// DeploysPerWeek counts successful deploys to production.
	DeploysPerWeek float64 `json:"deploys_per_week"`
	// ChangeFailureRate is the share of production deploys that failed, 0-1.
	ChangeFailureRate *float64 `json:"change_failure_rate,omitempty"`
	// RecoverySeconds is the median time from a failed production deploy to
	// the next one of that service that succeeded.
	RecoverySeconds *float64 `json:"recovery_seconds,omitempty"`
	// PromotionSeconds is the median time from an image being built in a
	// lower level to its promotion reaching production.
	PromotionSeconds *float64 `json:"promotion_seconds,omitempty"`
	// Projects is each project's deploys per day, every level together, for
	// the project list's sparkline; keyed by the project's id.
	Projects map[uuid.UUID][]int `json:"projects"`
}

// DeliveryDay is one day's deploys and job runs, by outcome.
type DeliveryDay struct {
	Date          string `json:"date"` // YYYY-MM-DD, UTC
	Deployed      int    `json:"deployed"`
	DeployFailed  int    `json:"deploy_failed"`
	JobsSucceeded int    `json:"jobs_succeeded"`
	JobsFailed    int    `json:"jobs_failed"`
}

// Get reads the overview for someone who can see projectIDs (every level of
// them included). Domains are an admin's to manage, and shown only to one.
func (s *OverviewService) Get(ctx context.Context, orgID uuid.UUID, projectIDs []uuid.UUID, isAdmin bool) (*Overview, error) {
	out := &Overview{Attention: []AttentionItem{}, Stats: map[string]map[string]int{}}
	if len(projectIDs) > 0 {
		stats, err := s.projects.Stats(ctx, projectIDs)
		if err != nil {
			return nil, err
		}
		out.Stats = stats
	}

	var projects []db.Project
	if len(projectIDs) > 0 {
		if err := s.db.WithContext(ctx).Where("id IN ?", projectIDs).Find(&projects).Error; err != nil {
			return nil, err
		}
	}
	delivery, err := s.delivery(ctx, projects, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	out.Delivery = *delivery
	where := map[uuid.UUID]string{} // a project, or "Demo Project (staging)" for a level
	for _, p := range projects {
		where[p.ID] = p.Name
		if p.ParentProjectID != nil {
			where[p.ID] = fmt.Sprintf("%s (%s)", p.Name, p.EnvName)
		}
	}
	add := func(item AttentionItem) { out.Attention = append(out.Attention, item) }
	id := func(u uuid.UUID) *uuid.UUID { return &u }

	if len(projectIDs) > 0 {
		// Services and databases that are down.
		var failed []db.Service
		if err := s.db.WithContext(ctx).Where("project_id IN ? AND status = ?", projectIDs, db.ServiceFailed).Order("name").Find(&failed).Error; err != nil {
			return nil, err
		}
		down := map[uuid.UUID]bool{}
		for _, sv := range failed {
			down[sv.ID] = true
			kind := "service_failed"
			if sv.Type == db.ServiceTypeDatabase {
				kind = "database_failed"
			}
			add(AttentionItem{Kind: kind, Severity: SeverityCritical,
				Title: fmt.Sprintf("%s is failing", sv.Name), Detail: "in " + where[sv.ProjectID],
				ProjectID: id(sv.ProjectID), ServiceID: id(sv.ID)})
		}

		// Services that keep dying: out of memory, crashing, cannot start.
		for _, p := range projects {
			troubles, err := s.workloads.Troubles(ctx, p.ID)
			if err != nil {
				continue
			}
			for sid, t := range troubles {
				if down[sid] {
					continue
				}
				var sv db.Service
				if s.db.WithContext(ctx).Select("id", "name", "project_id").First(&sv, "id = ?", sid).Error != nil {
					continue
				}
				down[sid] = true
				add(AttentionItem{Kind: "service_trouble", Severity: SeverityCritical,
					Title: troubleTitle(sv.Name, t), Detail: troubleDetail(t) + "; in " + where[sv.ProjectID],
					ProjectID: id(sv.ProjectID), ServiceID: id(sv.ID)})
			}
		}

		// A last deploy that failed on a service still running its previous
		// one: nothing is down, but the change did not go out.
		var recent []db.Deployment
		if err := s.db.WithContext(ctx).Select("id", "service_id", "status", "created_at").
			Where("service_id IN (?) AND created_at > ?",
				s.db.Model(&db.Service{}).Select("id").Where("project_id IN ?", projectIDs), time.Now().Add(-7*24*time.Hour)).
			Order("created_at DESC").Find(&recent).Error; err != nil {
			return nil, err
		}
		latest := map[uuid.UUID]bool{}
		for _, d := range recent {
			if latest[d.ServiceID] {
				continue // only each service's last deploy counts
			}
			latest[d.ServiceID] = true
			if d.Status != db.DeploymentFailed || down[d.ServiceID] {
				continue
			}
			var sv db.Service
			if s.db.WithContext(ctx).First(&sv, "id = ?", d.ServiceID).Error != nil {
				continue
			}
			add(AttentionItem{Kind: "deploy_failed", Severity: SeverityWarning,
				Title: fmt.Sprintf("The last deploy of %s failed", sv.Name), Detail: "in " + where[sv.ProjectID] + "; the previous one still runs",
				ProjectID: id(sv.ProjectID), ServiceID: id(sv.ID), DeploymentID: id(d.ID)})
		}

		// Advice from builds: a start command the builder could not find, too
		// little memory for what the app loads, a port it does not listen on.
		// A service already listed as down says it has suggestions waiting instead
		// of getting a second line.
		for _, p := range projects {
			hints, err := s.workloads.Hints(ctx, p.ID)
			if err != nil {
				continue
			}
			for sid, hs := range hints {
				fixes := fmt.Sprintf("%d %s on its page", len(hs), plural(len(hs), "suggestion", "suggestions"))
				listed := false
				for i := range out.Attention {
					if a := &out.Attention[i]; a.ServiceID != nil && *a.ServiceID == sid {
						a.Detail += "; " + fixes
						listed = true
					}
				}
				if listed {
					continue
				}
				var sv db.Service
				if s.db.WithContext(ctx).Select("id", "name", "project_id").First(&sv, "id = ?", sid).Error != nil {
					continue
				}
				titles := make([]string, len(hs))
				for i, h := range hs {
					titles[i] = strings.ToLower(h.Title[:1]) + h.Title[1:]
				}
				add(AttentionItem{Kind: "service_hints", Severity: SeverityWarning,
					Title:     fmt.Sprintf("%s: %s", sv.Name, joinNames(titles)),
					Detail:    fixes + "; in " + where[sv.ProjectID],
					ProjectID: id(sv.ProjectID), ServiceID: id(sv.ID)})
			}
		}

		// Jobs whose last run failed.
		var jobs []db.Job
		if err := s.db.WithContext(ctx).Where("project_id IN ? AND status = ?", projectIDs, db.JobStatusFailed).Order("name").Find(&jobs).Error; err != nil {
			return nil, err
		}
		for _, j := range jobs {
			add(AttentionItem{Kind: "job_failed", Severity: SeverityWarning,
				Title: fmt.Sprintf("The last run of %s failed", j.Name), Detail: "in " + where[j.ProjectID],
				ProjectID: id(j.ProjectID), JobID: id(j.ID)})
		}

		// Databases with nothing to restore from.
		var dbs []db.Service
		if err := s.db.WithContext(ctx).Where("project_id IN ? AND type = ?", projectIDs, db.ServiceTypeDatabase).Order("name").Find(&dbs).Error; err != nil {
			return nil, err
		}
		// Grouped per project: six databases without backups are one thing to
		// do, not six lines pushing everything else down.
		unprotected := map[uuid.UUID][]db.Service{}
		var order []uuid.UUID
		for _, d := range dbs {
			var bc db.BackupConfig
			err := s.db.WithContext(ctx).Where("service_id = ? AND enabled", d.ID).First(&bc).Error
			switch {
			case errors.Is(err, gorm.ErrRecordNotFound):
				if len(unprotected[d.ProjectID]) == 0 {
					order = append(order, d.ProjectID)
				}
				unprotected[d.ProjectID] = append(unprotected[d.ProjectID], d)
			case err != nil:
				return nil, err
			case bc.LastBackupStatus != nil && *bc.LastBackupStatus == db.BackupFailed:
				add(AttentionItem{Kind: "backup_failed", Severity: SeverityWarning,
					Title: fmt.Sprintf("The last backup of %s failed", d.Name), Detail: "in " + where[d.ProjectID],
					ProjectID: id(d.ProjectID), ServiceID: id(d.ID)})
			}
		}

		for _, pid := range order {
			list := unprotected[pid]
			if len(list) == 1 {
				d := list[0]
				add(AttentionItem{Kind: "backup_missing", Severity: SeverityWarning,
					Title: fmt.Sprintf("%s has no backups", d.Name), Detail: "in " + where[pid] + "; nothing to restore from if its volume is lost",
					ProjectID: id(pid), ServiceID: id(d.ID)})
				continue
			}
			names := make([]string, len(list))
			for i, d := range list {
				names[i] = d.Name
			}
			add(AttentionItem{Kind: "backup_missing", Severity: SeverityWarning,
				Title:  fmt.Sprintf("%d databases in %s have no backups", len(list), where[pid]),
				Detail: listNames(names, 3) + "; nothing to restore from if a volume is lost", ProjectID: id(pid)})
		}

		if err := s.environments(ctx, projects, add); err != nil {
			return nil, err
		}
	}

	// Every member sees the mesh's nodes on the overview already.
	var nodes []db.Node
	if err := s.db.WithContext(ctx).Where("organization_id = ? AND status <> ?", orgID, db.NodeOnline).Order("name").Find(&nodes).Error; err != nil {
		return nil, err
	}
	for _, n := range nodes {
		add(AttentionItem{Kind: "node_offline", Severity: SeverityCritical,
			Title: fmt.Sprintf("%s is %s", n.Name, n.Status), Detail: "its workloads cannot be scheduled there", NodeID: id(n.ID)})
	}
	for _, item := range s.diskItems(ctx, orgID) {
		add(item)
	}
	if isAdmin && s.orphans != nil {
		// What runs in the cluster with no service behind it: it holds memory,
		// and nothing in the console shows it except the Cluster page.
		orphans, err := s.orphans.List(ctx, orgID)
		if err == nil && len(orphans) > 0 {
			names := make([]string, len(orphans))
			for i, o := range orphans {
				names[i] = o.Name
			}
			title := fmt.Sprintf("%d workloads run that no service owns", len(orphans))
			if len(orphans) == 1 {
				title = fmt.Sprintf("%s runs, and no service owns it", orphans[0].Name)
			}
			add(AttentionItem{Kind: "orphans", Severity: SeverityWarning, Title: title,
				Detail: listNames(names, 3) + "; left over in the cluster, still using resources"})
		}
	}
	if isAdmin {
		var domains []db.Domain
		if err := s.db.WithContext(ctx).Where("organization_id = ?", orgID).Order("base_domain").Find(&domains).Error; err != nil {
			return nil, err
		}
		for _, d := range domains {
			switch {
			case !d.Verified && d.RetiringAt == nil:
				add(AttentionItem{Kind: "domain_unverified", Severity: SeverityWarning,
					Title: fmt.Sprintf("%s is not verified", d.BaseDomain), Detail: "routes cannot use it until its DNS record is found", DomainID: id(d.ID)})
			case d.FormerPrimary:
				add(AttentionItem{Kind: "former_primary", Severity: SeverityInfo,
					Title: fmt.Sprintf("%s still serves the platform", d.BaseDomain), Detail: "the former primary: remove it once nothing uses it", DomainID: id(d.ID)})
			}
		}
	}

	rank := map[string]int{SeverityCritical: 0, SeverityWarning: 1, SeverityInfo: 2}
	sort.SliceStable(out.Attention, func(i, j int) bool { return rank[out.Attention[i].Severity] < rank[out.Attention[j].Severity] })
	return out, nil
}

// environments adds what the projects' levels are waiting on: an image a
// level has that is newer than the one above runs (a promotion to make), and
// a level above an entry running something built there (a hotfix the level
// below has not caught up with).
func (s *OverviewService) environments(ctx context.Context, projects []db.Project, add func(AttentionItem)) error {
	seen := map[uuid.UUID]bool{}
	for _, p := range projects {
		root := p.ID
		if p.ParentProjectID != nil {
			root = *p.ParentProjectID
		}
		if seen[root] {
			continue
		}
		seen[root] = true
		board, err := s.promotions.Board(ctx, root)
		if err != nil {
			return err
		}
		if len(board.Levels) < 2 {
			continue
		}
		name := map[uuid.UUID]string{}
		for _, l := range board.Levels {
			name[l.ProjectID] = l.Name
		}
		rootID := root
		var rootProject db.Project
		if err := s.db.WithContext(ctx).Select("name").First(&rootProject, "id = ?", root).Error; err != nil {
			return err
		}
		for _, g := range board.Groups {
			at := func(level, lineage uuid.UUID) *BoardCell {
				for i := range g.Cells {
					if g.Cells[i].LevelID == level && g.Cells[i].LineageID == lineage {
						return &g.Cells[i]
					}
				}
				return nil
			}
			for i := 0; i+1 < len(g.Path); i++ {
				from, to := g.Path[i], g.Path[i+1]
				var ahead []string
				for _, c := range g.Cells {
					if c.LevelID != from || c.Image == "" || c.ImageBuiltAt == nil {
						continue
					}
					up := at(to, c.LineageID)
					if up == nil || up.Image == "" || up.ImageBuiltAt == nil ||
						(up.Image != c.Image && c.ImageBuiltAt.After(*up.ImageBuiltAt)) {
						ahead = append(ahead, c.ServiceName)
					}
				}
				if len(ahead) > 0 {
					add(AttentionItem{Kind: "promotion_waiting", Severity: SeverityInfo,
						Title:     fmt.Sprintf("%s in %s is ready for %s", joinNames(ahead), name[from], name[to]),
						Detail:    fmt.Sprintf("%s, group %s", rootProject.Name, g.Name),
						ProjectID: &rootID})
				}
			}
			for _, c := range g.Cells {
				if c.LevelID == g.Path[0] || (c.Arrival != db.DeploySourceBuild && c.Arrival != db.DeploySourceImage) {
					continue
				}
				level, service := c.LevelID, c.ServiceID
				add(AttentionItem{Kind: "hotfix_running", Severity: SeverityInfo,
					Title:     fmt.Sprintf("%s in %s (%s) runs something built there", c.ServiceName, rootProject.Name, name[c.LevelID]),
					Detail:    fmt.Sprintf("a hotfix that %s's next build must include, or that an overwrite replaces", name[g.Path[0]]),
					ProjectID: &level, ServiceID: &service})
			}
		}
	}
	return nil
}

func joinNames(names []string) string {
	switch len(names) {
	case 1:
		return names[0]
	case 2:
		return names[0] + " and " + names[1]
	}
	return fmt.Sprintf("%s and %d more", names[0], len(names)-1)
}

// listNames names up to max, then counts the rest.
func listNames(names []string, max int) string {
	if len(names) <= max {
		return strings.Join(names, ", ")
	}
	return fmt.Sprintf("%s and %d more", strings.Join(names[:max], ", "), len(names)-max)
}

// delivery reads the last DeliveryDays days of deployments and job runs in
// projects (levels included) as of now.
func (s *OverviewService) delivery(ctx context.Context, projects []db.Project, now time.Time) (*Delivery, error) {
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -(DeliveryDays - 1))
	out := &Delivery{Days: make([]DeliveryDay, DeliveryDays), Projects: map[uuid.UUID][]int{}}
	for i := range out.Days {
		out.Days[i].Date = start.AddDate(0, 0, i).Format("2006-01-02")
	}
	dayOf := func(t time.Time) int { return int(t.UTC().Sub(start).Hours() / 24) }
	if len(projects) == 0 {
		return out, nil
	}
	ids := make([]uuid.UUID, len(projects))
	rootOf := map[uuid.UUID]uuid.UUID{}
	production := map[uuid.UUID]bool{}
	for i, p := range projects {
		ids[i] = p.ID
		rootOf[p.ID] = p.ID
		if p.ParentProjectID != nil {
			rootOf[p.ID] = *p.ParentProjectID
		} else {
			production[p.ID] = true
		}
	}

	type deployRow struct {
		db.Deployment
		ProjectID uuid.UUID
	}
	var deps []deployRow
	if err := s.db.WithContext(ctx).Table("deployments").
		Select("deployments.*, services.project_id AS project_id").
		Joins("JOIN services ON services.id = deployments.service_id").
		Where("services.project_id IN ? AND deployments.created_at >= ?", ids, start).
		Order("deployments.created_at").Scan(&deps).Error; err != nil {
		return nil, err
	}
	var prodOK, prodFailed int
	var recoveries, promotions []float64
	for i, d := range deps {
		day := dayOf(d.CreatedAt)
		if day < 0 || day >= DeliveryDays {
			continue
		}
		switch d.Status {
		case db.DeploymentSuccess:
			out.Days[day].Deployed++
		case db.DeploymentFailed:
			out.Days[day].DeployFailed++
		default:
			continue // still running: counted once it ends
		}
		root := rootOf[d.ProjectID]
		if out.Projects[root] == nil {
			out.Projects[root] = make([]int, DeliveryDays)
		}
		out.Projects[root][day]++
		if !production[d.ProjectID] {
			continue
		}
		if d.Status == db.DeploymentFailed {
			prodFailed++
			for _, later := range deps[i+1:] {
				if later.ServiceID == d.ServiceID && later.Status == db.DeploymentSuccess {
					recoveries = append(recoveries, later.CreatedAt.Sub(d.CreatedAt).Seconds())
					break
				}
			}
			continue
		}
		prodOK++
		if d.Source == db.DeploySourcePromotion {
			origin := s.promotions.deployments.ImageOrigin(ctx, d.Deployment)
			promotions = append(promotions, d.CreatedAt.Sub(origin.BuiltAt).Seconds())
		}
	}
	out.DeploysPerWeek = float64(prodOK) * 7 / DeliveryDays
	if total := prodOK + prodFailed; total > 0 {
		rate := float64(prodFailed) / float64(total)
		out.ChangeFailureRate = &rate
	}
	out.RecoverySeconds = median(recoveries)
	out.PromotionSeconds = median(promotions)

	type runRow struct {
		Status    db.JobStatus
		CreatedAt time.Time
	}
	var runs []runRow
	if err := s.db.WithContext(ctx).Table("job_runs").
		Select("job_runs.status, job_runs.created_at").
		Joins("JOIN jobs ON jobs.id = job_runs.job_id").
		Where("jobs.project_id IN ? AND job_runs.created_at >= ?", ids, start).
		Scan(&runs).Error; err != nil {
		return nil, err
	}
	for _, r := range runs {
		day := dayOf(r.CreatedAt)
		if day < 0 || day >= DeliveryDays {
			continue
		}
		switch r.Status {
		case db.JobStatusSuccess:
			out.Days[day].JobsSucceeded++
		case db.JobStatusFailed:
			out.Days[day].JobsFailed++
		}
	}
	return out, nil
}

// median is nil for nothing to take one of.
func median(xs []float64) *float64 {
	if len(xs) == 0 {
		return nil
	}
	sorted := append([]float64(nil), xs...)
	sort.Float64s(sorted)
	m := sorted[len(sorted)/2]
	if len(sorted)%2 == 0 {
		m = (sorted[len(sorted)/2-1] + m) / 2
	}
	return &m
}

// diskItems reports online nodes whose disk is filling. Each node is read at
// most once a minute, all at once, and a node that cannot be read (no
// node_exporter, a Windows machine) is left out rather than guessed.
func (s *OverviewService) diskItems(ctx context.Context, orgID uuid.UUID) []AttentionItem {
	if s.metrics == nil {
		return nil
	}
	var nodes []db.Node
	if err := s.db.WithContext(ctx).Where("organization_id = ? AND status = ?", orgID, db.NodeOnline).Order("name").Find(&nodes).Error; err != nil {
		return nil
	}
	s.diskMu.Lock()
	if s.diskCache == nil {
		s.diskCache = map[uuid.UUID]diskReading{}
	}
	var stale []db.Node
	for _, n := range nodes {
		if r, ok := s.diskCache[n.ID]; !ok || time.Since(r.at) > diskReadingTTL {
			stale = append(stale, n)
		}
	}
	s.diskMu.Unlock()

	var wg sync.WaitGroup
	for _, n := range stale {
		wg.Add(1)
		go func(n db.Node) {
			defer wg.Done()
			rctx, cancel := context.WithTimeout(ctx, 3*time.Second)
			defer cancel()
			reading := diskReading{at: time.Now()}
			if m, err := s.metrics(rctx, n.ID); err == nil && m != nil {
				reading.total, reading.avail = m.DiskTotalBytes, m.DiskAvailBytes
			}
			s.diskMu.Lock()
			s.diskCache[n.ID] = reading
			s.diskMu.Unlock()
		}(n)
	}
	wg.Wait()

	var out []AttentionItem
	s.diskMu.Lock()
	defer s.diskMu.Unlock()
	for _, n := range nodes {
		r := s.diskCache[n.ID]
		if r.total <= 0 {
			continue
		}
		used := int(100 * (r.total - r.avail) / r.total)
		if used < diskWarnPercent {
			continue
		}
		severity := SeverityWarning
		if used >= diskCriticalPercent {
			severity = SeverityCritical
		}
		id := n.ID
		out = append(out, AttentionItem{Kind: "node_disk", Severity: severity,
			Title:  fmt.Sprintf("%s's disk is %d%% full", n.Name, used),
			Detail: fmt.Sprintf("%s free; builds and databases fail when it fills. Build caches and volumes are the usual cause", humanBytes(r.avail)),
			NodeID: &id})
	}
	return out
}

func humanBytes(b int64) string {
	const gb = 1 << 30
	if b >= gb {
		return fmt.Sprintf("%.0f GB", float64(b)/gb)
	}
	return fmt.Sprintf("%.0f MB", float64(b)/(1<<20))
}

// troubleTitle and troubleDetail say what is wrong with a service in words.
func troubleTitle(name string, t *Trouble) string {
	switch t.Kind {
	case TroubleOutOfMemory:
		return name + " keeps running out of memory"
	case TroubleImagePull:
		return name + " cannot pull its image"
	case TroubleCannotStart:
		return name + " cannot start"
	}
	return name + " keeps stopping"
}

func troubleDetail(t *Trouble) string {
	d := fmt.Sprintf("%d restarts", t.Restarts)
	if t.Restarts == 1 {
		d = "1 restart"
	}
	switch t.Kind {
	case TroubleOutOfMemory:
		if t.MemoryLimit != "" {
			d += "; memory limit " + t.MemoryLimit
		}
	case TroubleCrashing:
		d += fmt.Sprintf("; exits with code %d", t.ExitCode)
	case TroubleImagePull, TroubleCannotStart:
		if t.Message != "" {
			d = t.Message
		}
	}
	return d
}

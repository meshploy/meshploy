package db

import (
	"sync"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// EdgeVersion counts the changes to something the edge proxy keeps in memory.
// The proxy reads the count every second, which is one row, and reloads only
// when it moved: a change takes effect within a second, without the proxy
// asking the database on every request or waiting out a long refresh.
//
// A count moves with every write GORM makes to a table watched for it (see
// WatchEdgeTables), in the same transaction as the write, so no code path that
// changes what the proxy serves can forget to say so.
type EdgeVersion struct {
	Name      string `gorm:"primaryKey"`
	Version   int64  `gorm:"not null;default:0"`
	UpdatedAt time.Time
}

func (EdgeVersion) TableName() string { return "edge_versions" }

// EdgeRoutes is the count of changes to what the proxy routes: hostnames,
// their targets, published TCP ports and a migration's fallback edge.
const EdgeRoutes = "routes"

var (
	edgeMu     sync.RWMutex
	edgeTables = map[string][]string{
		"routes":         {EdgeRoutes},
		"route_targets":  {EdgeRoutes},
		"tcp_routes":     {EdgeRoutes},
		"edge_fallbacks": {EdgeRoutes},
	}
)

// WatchEdgeTables counts every write to tables under the count name: an
// extension whose part of the proxy caches something registers what that
// depends on, from init().
func WatchEdgeTables(name string, tables ...string) {
	edgeMu.Lock()
	defer edgeMu.Unlock()
	for _, t := range tables {
		edgeTables[t] = append(edgeTables[t], name)
	}
}

func countsFor(table string) []string {
	edgeMu.RLock()
	defer edgeMu.RUnlock()
	return edgeTables[table]
}

const edgeCallback = "meshploy:edge_versions"

// countEdgeChanges makes database count writes to watched tables. Called for
// every connection Open makes and by Migrate, so a connection made some other
// way (a test's) counts once it is migrated; registering twice is harmless.
func countEdgeChanges(database *gorm.DB) error {
	cb := database.Callback()
	if cb.Create().Get(edgeCallback) != nil {
		return nil
	}
	if err := cb.Create().After("gorm:create").Register(edgeCallback, bumpEdgeVersions); err != nil {
		return err
	}
	if err := cb.Update().After("gorm:update").Register(edgeCallback, bumpEdgeVersions); err != nil {
		return err
	}
	return cb.Delete().After("gorm:delete").Register(edgeCallback, bumpEdgeVersions)
}

func bumpEdgeVersions(tx *gorm.DB) {
	if tx.Error != nil || tx.Statement.RowsAffected == 0 || tx.Statement.Table == "" {
		return
	}
	names := countsFor(tx.Statement.Table)
	if len(names) == 0 {
		return
	}
	// The same connection, so inside the write's transaction when it has one:
	// the count moves when the change commits, and not if it rolls back.
	s := tx.Session(&gorm.Session{NewDB: true, SkipHooks: true})
	for _, n := range names {
		if err := s.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "name"}},
			DoUpdates: clause.Assignments(map[string]any{"version": gorm.Expr("edge_versions.version + 1"), "updated_at": time.Now()}),
		}).Create(&EdgeVersion{Name: n, Version: 1, UpdatedAt: time.Now()}).Error; err != nil {
			_ = tx.AddError(err)
			return
		}
	}
}

// EdgeVersionOf is a count's current value; zero before its first change.
func EdgeVersionOf(database *gorm.DB, name string) (int64, error) {
	var v EdgeVersion
	err := database.Where("name = ?", name).Limit(1).Find(&v).Error
	return v.Version, err
}

// EdgeCheckEvery is how often the proxy reads a count to see if it moved.
const EdgeCheckEvery = time.Second

// FollowEdgeVersion calls reload whenever the named count moves, read every
// EdgeCheckEvery, and every full regardless, in case a change reached the
// database some other way than through GORM. It returns at once; the loop runs
// for the life of the process. The caller loads once itself before calling;
// the first check reloads again, so a change made between that load and this
// call is not left for the full interval.
func FollowEdgeVersion(database *gorm.DB, name string, full time.Duration, reload func()) {
	seen := int64(-1)
	go func() {
		last := time.Now()
		t := time.NewTicker(EdgeCheckEvery)
		defer t.Stop()
		for range t.C {
			v, err := EdgeVersionOf(database, name)
			if (err == nil && v != seen) || time.Since(last) >= full {
				if err == nil {
					seen = v
				}
				last = time.Now()
				reload()
			}
		}
	}()
}

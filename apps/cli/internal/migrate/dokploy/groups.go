package dokploy

import (
	"fmt"
	"sort"
	"strings"
)

// Rough copy speeds for the downtime estimate. A dump and restore goes through
// the database engine; a volume copy is a file copy on the same disk.
const (
	dumpMBPerMinute   = 500
	volumeMBPerMinute = 2000
)

// Group is what moves together, so no data lives in two places at once: a
// database with every app that uses it, or an app with no database.
type Group struct {
	ID      string        `json:"id"`
	Name    string        `json:"name"`
	Members []GroupMember `json:"members"`
	Data    []GroupData   `json:"data"`
	// Downtime is an estimate of how long the group's apps are unavailable
	// while it moves: a restart, plus the time to copy its data.
	Downtime string `json:"downtime"`
	// CanMove is false while a member has a decision with no choice.
	CanMove  bool     `json:"can_move"`
	Blockers []string `json:"blockers,omitempty"`
	Notes    []string `json:"notes,omitempty"`
}

// GroupMember is one item of a group.
type GroupMember struct {
	Kind    string `json:"kind"`
	ID      string `json:"id"`
	Name    string `json:"name"`
	Project string `json:"project,omitempty"`
}

// GroupData is data a group copies while it moves.
type GroupData struct {
	Name string `json:"name"`
	MB   int    `json:"mb"`
	Move string `json:"move"`
}

// buildGroups joins each database with the apps that reference it. A
// reference is the database's appName, its hostname on Dokploy's network,
// appearing in an app's env, its environment's or project's shared variables,
// or its stored compose file. Those values are searched in memory only.
func (b *builder) buildGroups() []Group {
	byID := map[string]Item{}
	var order []string
	for _, it := range b.items {
		switch it.Kind {
		case "application", "compose", "database":
			if it.Verdict == NotMoved {
				continue
			}
			byID[it.ID] = it
			order = append(order, it.ID)
		}
	}

	// Union-find over items: an app referencing two databases joins them.
	parent := map[string]string{}
	var find func(string) string
	find = func(x string) string {
		if parent[x] == "" || parent[x] == x {
			parent[x] = x
			return x
		}
		parent[x] = find(parent[x])
		return parent[x]
	}
	union := func(a, c string) { parent[find(a)] = find(c) }

	type db struct{ id, appName string }
	var dbs []db
	for _, e := range engines {
		for _, r := range b.rows[e.table] {
			if _, ok := byID[r.Str(e.id)]; ok && r.Str("appName") != "" {
				dbs = append(dbs, db{r.Str(e.id), r.Str("appName")})
			}
		}
	}
	// Apps that bind-mount the same host folder share its data, so they move
	// together and copy it into one volume they both mount.
	for _, users := range b.bindUsers {
		for _, id := range users[1:] {
			_, a := byID[users[0]]
			_, c := byID[id]
			if a && c {
				union(users[0], id)
			}
		}
	}

	referenced := map[string]bool{}
	for _, app := range b.appTexts() {
		if _, ok := byID[app.id]; !ok {
			continue
		}
		for _, d := range dbs {
			if strings.Contains(app.text, d.appName) {
				union(app.id, d.id)
				referenced[d.id] = true
			}
		}
	}

	members := map[string][]string{}
	var roots []string
	for _, id := range order {
		root := find(id)
		if len(members[root]) == 0 {
			roots = append(roots, root)
		}
		members[root] = append(members[root], id)
	}

	b.settleSharedMounts(find)
	for i := range b.items {
		if _, ok := byID[b.items[i].ID]; ok {
			byID[b.items[i].ID] = b.items[i]
		}
	}

	groups := make([]Group, 0, len(roots))
	for _, root := range roots {
		g := Group{CanMove: true, Data: []GroupData{}}
		copyMinutes := 0.0
		var dbNames, appNames []string
		for _, id := range members[root] {
			it := byID[id]
			g.Members = append(g.Members, GroupMember{Kind: it.Kind, ID: it.ID, Name: it.Name, Project: it.Project})
			if it.Kind == "database" {
				dbNames = append(dbNames, it.Name)
				if !referenced[id] {
					g.Notes = append(g.Notes, "no app names "+it.Name+" by its Dokploy hostname; if one reaches it by address or published port, move them together")
				}
				if mb, ok := atoi(it.Details["data_mb"]); ok {
					move := it.Details["data_move"]
					g.Data = append(g.Data, GroupData{Name: it.Name, MB: mb, Move: move})
					if strings.HasPrefix(move, "volume") {
						copyMinutes += float64(mb) / volumeMBPerMinute
					} else {
						copyMinutes += float64(mb) / dumpMBPerMinute
					}
				}
			} else {
				appNames = append(appNames, it.Name)
				for _, d := range b.itemData[id] {
					g.Data = append(g.Data, d)
					copyMinutes += float64(d.MB) / volumeMBPerMinute
				}
			}
			for _, d := range it.Decisions {
				if d.Default != "" {
					continue
				}
				g.CanMove = false
				g.Blockers = append(g.Blockers, it.Name+": "+d.Question)
			}
			for _, d := range it.Decisions {
				if strings.HasPrefix(d.ID, "mount:") && d.Default == "copy" {
					path := strings.TrimPrefix(d.ID, "mount:")
					if hasData(g.Data, path) {
						continue // a folder two members share is copied once
					}
					mb := b.src.PathMB[path]
					g.Data = append(g.Data, GroupData{Name: path, MB: mb, Move: "copy into a volume"})
					copyMinutes += float64(mb) / volumeMBPerMinute
				}
			}
		}
		sort.Strings(dbNames)
		sort.Strings(appNames)
		project := g.Members[0].Project
		for _, m := range g.Members {
			if m.Project != project {
				project = ""
			}
		}
		switch {
		case len(dbNames) > 0 && len(appNames) > 0:
			g.Name = strings.Join(dbNames, " + ") + " with " + strings.Join(appNames, ", ")
		case len(dbNames) > 0:
			g.Name = strings.Join(dbNames, " + ")
		default:
			g.Name = strings.Join(appNames, ", ")
		}
		if project != "" {
			g.Name = project + " / " + g.Name
		}
		g.ID = "g-" + shortID(members[root])
		g.Downtime = downtimeEstimate(copyMinutes, len(g.Data) > 0)
		groups = append(groups, g)
	}
	sort.SliceStable(groups, func(i, j int) bool {
		// Groups that can move and carry no data first: they are the easy ones.
		if groups[i].CanMove != groups[j].CanMove {
			return groups[i].CanMove
		}
		if (len(groups[i].Data) == 0) != (len(groups[j].Data) == 0) {
			return len(groups[i].Data) == 0
		}
		return groups[i].Name < groups[j].Name
	})
	return groups
}

// settleSharedMounts gives a shared folder's decision a default when every
// container that mounts it belongs to the same group: the group copies it into
// one volume its members share. Shared with anything outside the group, it
// still needs a choice.
func (b *builder) settleSharedMounts(find func(string) string) {
	owner := map[string]string{} // appName -> item id
	for _, it := range b.items {
		if n := it.Details["app_name"]; n != "" {
			owner[n] = it.ID
		}
	}
	for i := range b.items {
		it := &b.items[i]
		for j := range it.Decisions {
			d := &it.Decisions[j]
			if !strings.HasPrefix(d.ID, "mount:") || d.Default != "" {
				continue
			}
			path := strings.TrimPrefix(d.ID, "mount:")
			inGroup := true
			var sharers []string
			for _, c := range b.src.Docker.Containers {
				if !c.MountsPath(path) {
					continue
				}
				id := owner[c.Service]
				if id == "" {
					id = owner[c.Project]
				}
				if id == "" || find(id) != find(it.ID) {
					inGroup = false
					break
				}
				if id != it.ID {
					sharers = append(sharers, b.itemName(id))
				}
			}
			if inGroup {
				d.Default = "copy"
				d.Question = "Bind mount of " + path + ", shared with " + strings.Join(dedupe(sharers), ", ") +
					", which moves in the same group: copied once into a volume both mount"
			}
		}
	}
}

func (b *builder) itemName(id string) string {
	for _, it := range b.items {
		if it.ID == id {
			return it.Name
		}
	}
	return id
}

func hasData(list []GroupData, name string) bool {
	for _, d := range list {
		if d.Name == name {
			return true
		}
	}
	return false
}

type appText struct{ id, text string }

// appTexts gathers, per application and compose app, every text a database
// reference could be in. Held in memory while grouping, never output.
func (b *builder) appTexts() []appText {
	shared := func(r Row) string {
		env := b.env[r.Str("environmentId")]
		return env.Str("env") + "\n" + b.project[env.Str("projectId")].Str("env")
	}
	var out []appText
	for _, r := range b.rows["application"] {
		id := r.Str("applicationId")
		out = append(out, appText{id, strings.Join([]string{
			b.runningEnv(id), r.Str("env"), r.Str("buildArgs"), shared(r),
		}, "\n")})
	}
	for _, r := range b.rows["compose"] {
		id := r.Str("composeId")
		out = append(out, appText{id, strings.Join([]string{
			b.runningEnv(id), r.Str("env"), r.Str("composeFile"), shared(r),
		}, "\n")})
	}
	return out
}

// runningEnv is what this workload runs with, from Docker.
//
// Without it a database is never grouped with the applications that use it on
// any Dokploy that encrypts the env column - which current ones do. The
// grouping searches for the database's hostname in an application's
// environment, and ciphertext contains no hostname, so every database came out
// as a group of its own and an application could move away from its data.
func (b *builder) runningEnv(itemID string) string {
	name := appNameIn(b.rows, itemID)
	if name == "" {
		return ""
	}
	for _, s := range b.src.Docker.Services {
		if s.Name == name {
			return strings.Join(s.Env, "\n")
		}
	}
	var lines []string
	for _, c := range b.src.Docker.Containers {
		if c.Name == name || c.Service == name || c.Project == name {
			lines = append(lines, c.Env...)
		}
	}
	return strings.Join(lines, "\n")
}

func downtimeEstimate(copyMinutes float64, hasData bool) string {
	if !hasData {
		return "a restart, usually under a minute"
	}
	if copyMinutes < 1 {
		return "a restart plus under a minute to copy data"
	}
	return fmt.Sprintf("a restart plus about %d minutes to copy data", int(copyMinutes+0.999))
}

func shortID(ids []string) string {
	sorted := append([]string(nil), ids...)
	sort.Strings(sorted)
	h := uint32(2166136261)
	for _, id := range sorted {
		for i := 0; i < len(id); i++ {
			h ^= uint32(id[i])
			h *= 16777619
		}
	}
	return fmt.Sprintf("%08x", h)
}

func atoi(s string) (int, bool) {
	n := 0
	if s == "" {
		return 0, false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	return n, true
}

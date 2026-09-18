// Package dokploy reads a Dokploy host and plans moving it into Meshploy. It
// only reads: nothing here changes Dokploy, Docker or the host.
//
// Dokploy's own database is the source of intent (what builds from which repo,
// which domain routes where). Rows are read as JSON, so a column one Dokploy
// version has and another lacks is simply absent, and only the fields a plan
// shows are kept: env values, passwords, tokens and keys are never copied out
// of the query result.
package dokploy

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/meshploy/apps/cli/internal/migrate"
)

// Supported drizzle migration levels: the oldest and newest Dokploy schemas
// the reader has been checked against (v0.26.1 and v0.30.6).
const (
	MinMigrations = 133
	MaxMigrations = 196
)

// Row is one Dokploy row, as returned by row_to_json.
type Row map[string]any

// Str returns a text column, or "".
func (r Row) Str(key string) string {
	switch v := r[key].(type) {
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case int:
		return strconv.Itoa(v)
	case bool:
		return strconv.FormatBool(v)
	}
	return ""
}

// Bool returns a boolean column, or false.
func (r Row) Bool(key string) bool {
	b, _ := r[key].(bool)
	return b
}

// Int returns a number column, or 0.
func (r Row) Int(key string) int {
	switch v := r[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	}
	return 0
}

// Lines counts the non-empty, non-comment lines of a text column, such as the
// number of variables in an env block, without keeping any of them.
func (r Row) Lines(key string) int {
	n := 0
	for _, line := range strings.Split(r.Str(key), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			n++
		}
	}
	return n
}

// Tables the plan reads. Rows of any other table are never fetched.
var Tables = []string{
	"project", "environment", "application", "compose",
	"postgres", "mysql", "mariadb", "mongo", "redis",
	"domain", "mount", "port", "redirect", "security",
	"git_provider", "registry", "destination", "backup", "server", "certificate",
}

// Source is everything read from the host: Dokploy's rows, and the generic
// inventory.
// Tagged so a server's reading can be written to a file and replayed: the
// golden plans in testdata are Sources, and the plan built from one is compared
// against a recorded plan.
type Source struct {
	Detection Detection          `json:"detection"`
	Rows      map[string][]Row   `json:"rows,omitempty"`
	Docker    migrate.Docker     `json:"docker"`
	Listeners []migrate.Listener `json:"listeners,omitempty"`
	Resources migrate.Resources  `json:"resources"`
	// DynamicFiles is how many route files Dokploy's Traefik has, and
	// AcmeBytes the size of its certificate store.
	DynamicFiles int   `json:"dynamic_files,omitempty"`
	AcmeBytes    int64 `json:"acme_bytes,omitempty"`
	// PathMB is the size of host paths apps bind-mount, measured with du.
	PathMB map[string]int `json:"path_mb,omitempty"`
}

// Detection is whether this is a Dokploy host, and which Dokploy.
type Detection struct {
	Dokploy    bool     `json:"dokploy"`
	Signals    []string `json:"signals,omitempty"`
	ImageTag   string   `json:"image_tag,omitempty"`
	Version    string   `json:"version,omitempty"`
	Migrations int      `json:"migrations"`
	// Supported is whether the schema level is one the reader was checked
	// against; the plan is refused otherwise.
	Supported   bool   `json:"supported"`
	SupportNote string `json:"support_note,omitempty"`

	postgres string // the dokploy-postgres container
}

// Detect decides whether this host runs Dokploy and reads its version.
func Detect(r migrate.Runner, docker migrate.Docker) Detection {
	var d Detection
	if info, err := os.Stat("/etc/dokploy"); err == nil && info.IsDir() {
		d.Signals = append(d.Signals, "/etc/dokploy exists")
	}
	if svc, ok := docker.Service("dokploy"); ok {
		d.Signals = append(d.Signals, "Swarm service dokploy")
		d.ImageTag = svc.Image
	}
	for _, c := range docker.Containers {
		if strings.HasPrefix(c.Name, "dokploy-postgres") && c.State == "running" {
			d.Signals = append(d.Signals, "container "+c.Name)
			d.postgres = c.Name
		}
	}
	d.Dokploy = len(d.Signals) > 0
	if !d.Dokploy {
		return d
	}

	for _, c := range docker.Containers {
		if c.Service == "dokploy" && c.State == "running" {
			if out, err := r.Output("docker", "exec", c.Name, "cat", "/app/package.json"); err == nil {
				var pkg struct{ Version string }
				if json.Unmarshal([]byte(out), &pkg) == nil {
					d.Version = pkg.Version
				}
			}
			break
		}
	}

	if d.postgres == "" {
		d.SupportNote = "Dokploy's database container (dokploy-postgres) is not running"
		return d
	}
	out, err := psql(r, d.postgres, "select count(*) from drizzle.__drizzle_migrations")
	if err != nil {
		d.SupportNote = "could not read Dokploy's schema level: " + err.Error()
		return d
	}
	d.Migrations, _ = strconv.Atoi(strings.TrimSpace(out))
	d.Supported = d.Migrations >= MinMigrations && d.Migrations <= MaxMigrations
	if !d.Supported {
		d.SupportNote = fmt.Sprintf("Dokploy schema level %d has not been checked; supported are %d to %d (Dokploy v0.26.1 to v0.30.6)", d.Migrations, MinMigrations, MaxMigrations)
	}
	return d
}

// Read loads every table the plan needs. A table the schema does not have is
// empty, not an error.
func Read(r migrate.Runner, d Detection) (map[string][]Row, error) {
	if !d.Supported {
		return nil, errors.New(d.SupportNote)
	}
	rows := map[string][]Row{}
	for _, table := range Tables {
		out, err := psql(r, d.postgres, fmt.Sprintf(`select row_to_json(t) from %q t`, table))
		if err != nil {
			if strings.Contains(err.Error(), "does not exist") {
				continue
			}
			return nil, fmt.Errorf("read %s: %w", table, err)
		}
		for _, line := range strings.Split(out, "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			var row Row
			if err := json.Unmarshal([]byte(line), &row); err != nil {
				return nil, fmt.Errorf("read %s: %w", table, err)
			}
			rows[table] = append(rows[table], row)
		}
	}
	return rows, nil
}

func psql(r migrate.Runner, container, query string) (string, error) {
	return r.Output("docker", "exec", container, "psql", "-U", "dokploy", "-d", "dokploy", "-At", "-c", query)
}

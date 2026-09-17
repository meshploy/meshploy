package dokploy

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/meshploy/apps/cli/internal/migrate"
)

// traefikDir is where Dokploy keeps Traefik's configuration. A var for tests.
var traefikDir = "/etc/dokploy/traefik/dynamic"

// CollectDetect reads what detection shows: Dokploy's version and schema, the
// edge and the resources, without reading Dokploy's rows.
func CollectDetect(r migrate.Runner) (Source, error) {
	return collect(r, false)
}

// Collect reads everything the plan needs from this host. Detection is filled
// even when the rest cannot be read, so the reason can be shown.
func Collect(r migrate.Runner) (Source, error) {
	return collect(r, true)
}

func collect(r migrate.Runner, withRows bool) (Source, error) {
	var src Source
	docker, err := migrate.ReadDocker(r)
	if err != nil {
		return src, err
	}
	src.Docker = docker
	src.Detection = Detect(r, docker)
	if !src.Detection.Dokploy {
		return src, nil
	}
	src.Resources = migrate.ReadResources(docker)
	if listeners, err := migrate.ReadListeners(r); err == nil {
		src.Listeners = listeners
	}
	if entries, err := os.ReadDir(traefikDir); err == nil {
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".yml") && e.Name() != "dokploy.yml" && e.Name() != "middlewares.yml" {
				src.DynamicFiles++
			}
		}
	}
	if info, err := os.Stat(filepath.Join(traefikDir, "acme.json")); err == nil {
		src.AcmeBytes = info.Size()
	}
	if !src.Detection.Supported || !withRows {
		return src, nil
	}
	rows, err := Read(r, src.Detection)
	if err != nil {
		return src, err
	}
	src.Rows = rows

	// The size of each host path an app bind-mounts outside Dokploy's folders,
	// which a copy into a Meshploy volume has to move.
	src.PathMB = map[string]int{}
	for _, m := range rows["mount"] {
		path := m.Str("hostPath")
		if m.Str("type") != "bind" || path == "" || strings.HasPrefix(path, "/etc/dokploy/") {
			continue
		}
		if _, done := src.PathMB[path]; done {
			continue
		}
		if out, err := r.Output("du", "-sm", path); err == nil {
			if f := strings.Fields(out); len(f) > 0 {
				mb, _ := strconv.Atoi(f[0])
				src.PathMB[path] = mb
			}
		}
	}
	return src, nil
}

// Command sync-builtin refreshes the embedded fallback snapshot
// (internal/templates/builtin) from the meshploy-templates repo.
//
// The embed is a curated *subset* of the catalog — the handful of staples a
// fresh or offline install shows before/without a live fetch. This tool pulls
// each listed template's meta.yaml + docker-compose.yml + icon from GitHub raw
// and writes them into the snapshot; the id list is authoritative (dirs not in
// it are pruned).
//
// Run it via `go generate ./...` (directive in internal/templates/embed.go) or
// directly from apps/api:
//
//	go run ./tools/sync-builtin -ids pgadmin,redis -out internal/templates/builtin
package main

import (
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/meshploy/apps/api/internal/templates"
)

func main() {
	repo := flag.String("repo", "meshploy/meshploy-templates", "owner/repo of the template catalog")
	ref := flag.String("ref", "main", "git ref")
	ids := flag.String("ids", "pgadmin", "comma-separated template ids to embed (authoritative)")
	out := flag.String("out", "builtin", "output dir for the embedded snapshot")
	flag.Parse()

	idList := splitCSV(*ids)
	if len(idList) == 0 {
		fatal("no ids given")
	}

	client := &http.Client{Timeout: 30 * time.Second}
	rawBase := fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/templates/", *repo, *ref)

	// Prune dirs not in the list so -ids fully determines the snapshot.
	keep := map[string]bool{}
	for _, id := range idList {
		keep[id] = true
	}
	if entries, err := os.ReadDir(*out); err == nil {
		for _, e := range entries {
			if e.IsDir() && !keep[e.Name()] {
				_ = os.RemoveAll(filepath.Join(*out, e.Name()))
				fmt.Printf("pruned %s\n", e.Name())
			}
		}
	}

	for _, id := range idList {
		if err := syncOne(client, rawBase, *out, id); err != nil {
			fatal(fmt.Sprintf("%s: %v", id, err))
		}
		fmt.Printf("synced %s\n", id)
	}
	fmt.Printf("OK: %d template(s) embedded in %s\n", len(idList), *out)
}

func syncOne(c *http.Client, rawBase, out, id string) error {
	meta, err := fetch(c, rawBase+id+"/meta.yaml")
	if err != nil {
		return fmt.Errorf("meta.yaml: %w", err)
	}
	m, err := templates.ParseManifest(meta) // reuse the runtime validator + get the icon filename
	if err != nil {
		return err
	}
	compose, err := fetch(c, rawBase+id+"/docker-compose.yml")
	if err != nil {
		return fmt.Errorf("docker-compose.yml: %w", err)
	}
	iconName := m.Icon
	if iconName == "" {
		iconName = "logo.svg"
	}
	icon, err := fetch(c, rawBase+id+"/"+iconName)
	if err != nil {
		return fmt.Errorf("%s: %w", iconName, err)
	}

	dir := filepath.Join(out, id)
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	files := map[string][]byte{
		"meta.yaml":          meta,
		"docker-compose.yml": compose,
		iconName:             icon,
	}
	for name, b := range files {
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func fetch(c *http.Client, url string) ([]byte, error) {
	resp, err := c.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: status %d", url, resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}

func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func fatal(msg string) {
	fmt.Fprintln(os.Stderr, "sync-builtin: "+msg)
	os.Exit(1)
}

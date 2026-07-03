package templates

import (
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

// Catalog is the read surface the TemplateService depends on: list manifests,
// load a single template, and fetch its icon bytes. Both the local filesystem
// Registry and the GitHub-backed RemoteCatalog implement it, so the deploy
// engine is agnostic to where templates come from.
type Catalog interface {
	List() ([]*Manifest, error)
	Get(id string) (*Template, error)
	Icon(id string) (data []byte, contentType string, err error)
}

// iconContentType maps an icon filename to a MIME type for the HTTP response.
func iconContentType(name string) string {
	switch {
	case strings.HasSuffix(name, ".svg"):
		return "image/svg+xml"
	case strings.HasSuffix(name, ".png"):
		return "image/png"
	case strings.HasSuffix(name, ".jpg"), strings.HasSuffix(name, ".jpeg"):
		return "image/jpeg"
	case strings.HasSuffix(name, ".webp"):
		return "image/webp"
	default:
		return "application/octet-stream"
	}
}

// iconFilename returns the manifest's icon filename, defaulting to logo.svg.
func iconFilename(m *Manifest) string {
	if m != nil && m.Icon != "" {
		return m.Icon
	}
	return "logo.svg"
}

// Registry serves templates from a filesystem tree of `<root>/<id>/` directories
// (each with meta.yaml + docker-compose.yml). This is the minimal source used by
// the deploy engine today; the live-fetch + in-memory cache + pinned embed
// fallback layer on top of the same Get/List surface later.
type Registry struct {
	fsys fs.FS
	root string
}

// NewRegistry builds a registry over fsys, reading template dirs under root
// (e.g. os.DirFS("/etc/meshploy/templates"), "templates"). A nil registry is a
// valid "no catalog configured" state — callers guard for it.
func NewRegistry(fsys fs.FS, root string) *Registry {
	return &Registry{fsys: fsys, root: root}
}

// Get loads a single template by id.
func (r *Registry) Get(id string) (*Template, error) {
	if r == nil {
		return nil, fmt.Errorf("template registry not configured")
	}
	return Load(r.fsys, r.root+"/"+id)
}

// Icon reads a template's icon file from the filesystem.
func (r *Registry) Icon(id string) ([]byte, string, error) {
	if r == nil {
		return nil, "", fmt.Errorf("template registry not configured")
	}
	tpl, err := Load(r.fsys, r.root+"/"+id)
	if err != nil {
		return nil, "", err
	}
	name := iconFilename(tpl.Manifest)
	b, err := fs.ReadFile(r.fsys, r.root+"/"+id+"/"+name)
	if err != nil {
		return nil, "", err
	}
	return b, iconContentType(name), nil
}

// List loads every template's manifest, sorted by id. Malformed entries are
// skipped so one bad template does not break the catalog.
func (r *Registry) List() ([]*Manifest, error) {
	if r == nil {
		return []*Manifest{}, nil
	}
	entries, err := fs.ReadDir(r.fsys, r.root)
	if err != nil {
		return nil, fmt.Errorf("read template root: %w", err)
	}
	out := []*Manifest{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		tpl, err := Load(r.fsys, r.root+"/"+e.Name())
		if err != nil {
			continue // skip malformed templates
		}
		out = append(out, tpl.Manifest)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

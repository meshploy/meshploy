package templates

import (
	"fmt"
	"io/fs"
	"path"
)

// Template is a loaded template: its parsed manifest plus the raw, pre-
// substitution compose spec.
type Template struct {
	Manifest *Manifest
	Compose  string
}

// Load reads a template directory (meta.yaml + docker-compose.yml) from fsys.
// Used for embedded fixtures today and the on-disk registry cache later.
func Load(fsys fs.FS, dir string) (*Template, error) {
	metaB, err := fs.ReadFile(fsys, path.Join(dir, "meta.yaml"))
	if err != nil {
		return nil, fmt.Errorf("read meta.yaml: %w", err)
	}
	m, err := ParseManifest(metaB)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", dir, err)
	}
	composeB, err := fs.ReadFile(fsys, path.Join(dir, "docker-compose.yml"))
	if err != nil {
		return nil, fmt.Errorf("read docker-compose.yml: %w", err)
	}
	return &Template{Manifest: m, Compose: string(composeB)}, nil
}

// PrepareSpec is the end-to-end deploy-time conversion and the single place a
// template becomes a stack spec: resolve variables (prompted + generated +
// subdomains), then substitute them into the compose. It returns the Meshploy-
// ready stack spec, the full variable map (for the stack's encrypted columns),
// and the routes to create after Apply.
func (t *Template) PrepareSpec(promptValues map[string]string, baseDomain string) (spec string, vars map[string]string, exposes []ResolvedExpose, err error) {
	vars, exposes, err = Resolve(t.Manifest, promptValues, baseDomain)
	if err != nil {
		return "", nil, nil, err
	}
	spec, err = Substitute(t.Compose, vars)
	if err != nil {
		return "", nil, nil, err
	}
	return spec, vars, exposes, nil
}

// Package templates turns a one-click template (a docker-compose.yml + x-meshploy
// plus a meta.yaml) into a deployable Meshploy stack spec.
//
// Where conversion happens: because a template's compose already carries the
// x-meshploy blocks, it IS the Meshploy spec — there is no format conversion at
// deploy time. The only deploy-time transform is variable substitution
// (resolve generators + prompted values → replace ${VAR}). The heavy
// compose→DB-records reconciliation is the existing StackService.Apply, not here.
// Format conversion from foreign formats (e.g. Dokploy) happens offline in the
// meshploy-templates repo tooling, never at runtime.
package templates

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// Manifest is a parsed meta.yaml. JSON tags mirror the API response shape.
type Manifest struct {
	ID          string     `yaml:"id"          json:"id"`
	Name        string     `yaml:"name"        json:"name"`
	Description string     `yaml:"description" json:"description"`
	Category    string     `yaml:"category"    json:"category"`
	Version     string     `yaml:"version"     json:"version"`
	Icon        string     `yaml:"icon"        json:"icon"`
	Links       Links      `yaml:"links"       json:"links"`
	Maintainers []string   `yaml:"maintainers" json:"maintainers,omitempty"`
	Variables   []Variable `yaml:"variables"   json:"variables"`
}

type Links struct {
	Website string `yaml:"website" json:"website,omitempty"`
	Source  string `yaml:"source"  json:"source,omitempty"`
}

// Variable is one deploy-time input: either prompted (user-supplied) or
// generated. A subdomain generator carries an Expose block (web-facing
// service/port) and drives routing rather than env substitution. Only
// declarations are exposed over the API — never resolved values.
type Variable struct {
	Key      string  `yaml:"key"      json:"key"`
	Prompt   string  `yaml:"prompt"   json:"prompt,omitempty"`
	Required bool    `yaml:"required" json:"required,omitempty"`
	Generate string  `yaml:"generate" json:"generate,omitempty"`
	Expose   *Expose `yaml:"expose"   json:"expose,omitempty"`
}

type Expose struct {
	Service string `yaml:"service" json:"service"`
	Port    int    `yaml:"port"    json:"port"`
}

func (v Variable) IsPrompt() bool    { return v.Prompt != "" }
func (v Variable) IsGenerated() bool  { return v.Generate != "" }
func (v Variable) IsSubdomain() bool { return v.Generate == genSubdomain }

// ParseManifest decodes and validates a meta.yaml.
func ParseManifest(b []byte) (*Manifest, error) {
	var m Manifest
	if err := yaml.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	if m.ID == "" {
		return nil, fmt.Errorf("manifest: id is required")
	}
	seen := map[string]bool{}
	for i, v := range m.Variables {
		if v.Key == "" {
			return nil, fmt.Errorf("manifest: variable %d has no key", i)
		}
		if seen[v.Key] {
			return nil, fmt.Errorf("manifest: duplicate variable key %q", v.Key)
		}
		seen[v.Key] = true
		if v.IsPrompt() == v.IsGenerated() {
			return nil, fmt.Errorf("manifest: variable %q must be exactly one of prompt or generate", v.Key)
		}
	}
	return &m, nil
}

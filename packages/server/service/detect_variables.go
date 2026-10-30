package service

import (
	"path"
	"regexp"
	"sort"
	"strings"
)

// FoundVariable is a variable an app's code or example file names, offered
// when the app is made so it starts with what it reads. Nothing found is
// required: code that reads a name may give it a default.
type FoundVariable struct {
	Name string `json:"name"`
	// From is "example" for a name in an example env file, "code" for one the
	// code reads.
	From string `json:"from"`
}

// exampleEnvFiles name a project's variables without their values.
var exampleEnvFiles = map[string]bool{".env.example": true, ".env.sample": true, ".env.template": true,
	"example.env": true, ".env.dist": true}

var (
	exampleLine = regexp.MustCompile(`(?m)^\s*(?:export\s+)?([A-Z][A-Z0-9_]*)\s*=`)
	// readsEnv are the ways common languages read a variable; the first
	// group that matched is its name.
	readsEnv = regexp.MustCompile(`process\.env\.([A-Z][A-Z0-9_]*)` +
		`|process\.env\[\s*['"]([A-Z][A-Z0-9_]*)['"]\s*\]` +
		`|os\.environ\[\s*['"]([A-Z][A-Z0-9_]*)['"]\s*\]` +
		`|os\.(?:environ\.get|getenv)\(\s*['"]([A-Z][A-Z0-9_]*)['"]` +
		`|os\.(?:Getenv|LookupEnv)\(\s*"([A-Z][A-Z0-9_]*)"` +
		`|ENV(?:\.fetch\(\s*|\[\s*)['"]([A-Z][A-Z0-9_]*)['"]` +
		`|getenv\(\s*['"]([A-Z][A-Z0-9_]*)['"]` +
		`|\$_ENV\[\s*['"]([A-Z][A-Z0-9_]*)['"]` +
		`|env::var\(\s*"([A-Z][A-Z0-9_]*)"` +
		`|System\.getenv\(\s*"([A-Z][A-Z0-9_]*)"`)
	codeExt = map[string]bool{".js": true, ".mjs": true, ".cjs": true, ".ts": true, ".mts": true, ".tsx": true, ".jsx": true,
		".py": true, ".go": true, ".rb": true, ".php": true, ".rs": true, ".java": true, ".kt": true}
	// ambientVariables are set by the system or the runtime, never by the
	// person deploying.
	ambientVariables = map[string]bool{"NODE_ENV": true, "PATH": true, "HOME": true, "HOSTNAME": true, "PWD": true,
		"USER": true, "SHELL": true, "TERM": true, "LANG": true, "TMPDIR": true, "TZ": true, "CI": true}
)

// maxFoundVariables keeps a large codebase's list one a person can read.
const maxFoundVariables = 40

// detectVariables lists the variables a repository's example env files name,
// then those its code reads, each once, the example's first.
func detectVariables(r repoView) []FoundVariable {
	seen := map[string]bool{}
	var example, code []string
	add := func(list *[]string, name string) {
		if name == "" || seen[name] || ambientVariables[name] {
			return
		}
		seen[name] = true
		*list = append(*list, name)
	}
	paths := make([]string, 0, len(r.sizes))
	for p := range r.sizes {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		if exampleEnvFiles[path.Base(p)] && !skipped(p) {
			for _, m := range exampleLine.FindAllStringSubmatch(r.read(p), -1) {
				add(&example, m[1])
			}
		}
	}
	for _, p := range paths {
		if !codeExt[path.Ext(p)] || skipped(p) || r.sizes[p] > maxDetectRead || strings.HasSuffix(p, ".min.js") {
			continue
		}
		for _, m := range readsEnv.FindAllStringSubmatch(r.read(p), -1) {
			for _, g := range m[1:] {
				if g != "" {
					add(&code, g)
					break
				}
			}
		}
	}
	sort.Strings(code)
	out := make([]FoundVariable, 0, len(example)+len(code))
	for _, n := range example {
		out = append(out, FoundVariable{Name: n, From: "example"})
	}
	for _, n := range code {
		out = append(out, FoundVariable{Name: n, From: "code"})
	}
	if len(out) > maxFoundVariables {
		out = out[:maxFoundVariables]
	}
	return out
}

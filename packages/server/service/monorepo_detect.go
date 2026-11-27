package service

import (
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
)

// A repository can hold more than one app: a monorepo, with a folder per app,
// or one app kept in a folder beside files that are not it. Building its root
// then builds the wrong thing, or nothing that starts. Detection looks below
// the root for folders with an app of their own, so the form can ask which one
// a service runs, and for folders recorded as another repository, whose files
// a build never sees.

// FoundApp is a folder below a repository's root that holds an app.
type FoundApp struct {
	Folder string `json:"folder"`
	// Summary says what it is, as a detection's does: Node.js · Dockerfile.
	Summary string `json:"summary,omitempty"`
}

// maxFoundApps is how many folders detection lists; a repository with more
// is still offered them, in the folder field.
const maxFoundApps = 12

// appManifests are the files whose folder holds an app of its own.
var appManifests = map[string]bool{
	"package.json": true, "requirements.txt": true, "pyproject.toml": true, "Pipfile": true,
	"go.mod": true, "Gemfile": true, "Cargo.toml": true, "composer.json": true, "Dockerfile": true,
}

// libraryDirs hold what a monorepo's apps share, or what is not an app at all.
// A folder under one counts only when it has a Dockerfile, which says it runs.
var libraryDirs = map[string]bool{
	"packages": true, "libs": true, "lib": true, "shared": true, "examples": true, "example": true,
	"docs": true, "fixtures": true, "e2e": true, "scripts": true, "tools": true, ".github": true,
}

var cargoWorkspace = regexp.MustCompile(`(?m)^\[workspace\]`)

// workspaceTool names what ties a repository's folders together, if anything.
func workspaceTool(r repoView) string {
	if r.has("package.json") {
		var pkg struct {
			Workspaces json.RawMessage `json:"workspaces"`
		}
		if json.Unmarshal([]byte(r.read("package.json")), &pkg) == nil && len(pkg.Workspaces) > 0 && string(pkg.Workspaces) != "null" {
			return "npm workspaces"
		}
	}
	for _, t := range [][2]string{
		{"pnpm-workspace.yaml", "pnpm workspaces"}, {"turbo.json", "Turborepo"}, {"nx.json", "Nx"},
		{"lerna.json", "Lerna"}, {"go.work", "a Go workspace"},
	} {
		if r.has(t[0]) {
			return t[1]
		}
	}
	if r.has("Cargo.toml") && cargoWorkspace.MatchString(r.read("Cargo.toml")) {
		return "a Cargo workspace"
	}
	return ""
}

// subView is the part of a repository inside folder, as if it were the root.
func subView(r repoView, folder string) repoView {
	prefix := folder + "/"
	sizes := map[string]int64{}
	for p, n := range r.sizes {
		if strings.HasPrefix(p, prefix) {
			sizes[strings.TrimPrefix(p, prefix)] = n
		}
	}
	var links []string
	for _, l := range r.links {
		if strings.HasPrefix(l, prefix) {
			links = append(links, strings.TrimPrefix(l, prefix))
		}
	}
	return repoView{sizes: sizes, links: links, read: func(p string) string { return r.read(prefix + p) }}
}

// findApps lists the folders, three deep at most, that hold an app: the
// shallowest of any nested pair, as apps/web holds apps/web/server.
func findApps(r repoView) []FoundApp {
	folders := map[string]bool{}
	for p := range r.sizes {
		dir := path.Dir(p)
		if dir == "." || strings.Count(dir, "/") > 2 || !appManifests[path.Base(p)] || skipped(p) {
			continue
		}
		if libraryDirs[strings.SplitN(dir, "/", 2)[0]] && !r.has(dir+"/Dockerfile") {
			continue
		}
		folders[dir] = true
	}
	var list []string
	for dir := range folders {
		nested := false
		for parent := path.Dir(dir); parent != "."; parent = path.Dir(parent) {
			if folders[parent] {
				nested = true
				break
			}
		}
		if !nested {
			list = append(list, dir)
		}
	}
	sort.Strings(list)
	if len(list) > maxFoundApps {
		list = list[:maxFoundApps]
	}
	apps := make([]FoundApp, 0, len(list))
	for _, dir := range list {
		apps = append(apps, FoundApp{Folder: dir, Summary: suggest(detectStack(subView(r, dir), "")).Summary})
	}
	return apps
}

// appAtRoot is whether a repository's root holds an app by itself: one that
// says how it starts, or a language whose build makes one that does.
func appAtRoot(r repoView) bool {
	if r.has("Dockerfile") {
		return true
	}
	f := detectStack(r, "")
	if f.Framework != "" {
		return true
	}
	for _, l := range f.Languages {
		if l != "node" && l != "python" {
			return true
		}
	}
	if r.has("package.json") {
		var pkg struct {
			Scripts map[string]string `json:"scripts"`
		}
		_ = json.Unmarshal([]byte(r.read("package.json")), &pkg)
		// A build script is how a static site or a framework app is made,
		// which Railpack serves or starts itself.
		return pkg.Scripts["start"] != "" || pkg.Scripts["build"] != ""
	}
	return false
}

var gitmodulePath = regexp.MustCompile(`(?m)^\s*path\s*=\s*(.+?)\s*$`)

// detectLayout adds what a repository's layout says to a detection: its
// apps when it holds more than the root, and the folders a build cannot see.
// folder is the one being built; once one is chosen, nobody is asked to.
func detectLayout(d *Detection, r repoView, folder string) {
	d.Workspace = workspaceTool(r)
	apps := findApps(r)
	if d.Workspace != "" || len(apps) > 1 || (len(apps) == 1 && !appAtRoot(r)) {
		d.Apps = apps
	}
	switch {
	case folder != ".":
	case len(d.Apps) > 1:
		d.Notes = append(d.Notes, fmt.Sprintf("This repository holds %d apps. Choose the folder of the one this service runs.", len(d.Apps)))
	case len(d.Apps) == 1:
		d.Notes = append(d.Notes, fmt.Sprintf("The app looks to be in %s, not at the repository's root. Build from that folder.", d.Apps[0].Folder))
	}

	declared := map[string]bool{}
	for _, m := range gitmodulePath.FindAllStringSubmatch(r.read(".gitmodules"), -1) {
		declared[strings.Trim(m[1], `"/`)] = true
	}
	for _, l := range r.links {
		d.Unfetched = append(d.Unfetched, l)
		if declared[l] {
			d.Notes = append(d.Notes, fmt.Sprintf("%s is a git submodule, which builds do not fetch, so it is empty there. Deploy its own repository, or commit its files here.", l))
		} else {
			d.Notes = append(d.Notes, fmt.Sprintf("%s holds a repository of its own that was never added, so its files are not in this one. Remove %s/.git, then add and push the folder.", l, l))
		}
	}
}

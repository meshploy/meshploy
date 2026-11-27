package service

import (
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"
)

// An app in a JavaScript workspace often imports the workspace's own
// packages (packages/ui, packages/config), which live outside its folder and
// are linked in by the package manager's install at the repository's root.
// Built from its folder alone, such an app cannot find them. It is built from
// the root instead, with the install the workspace expects and build and
// start commands narrowed to the one app, in the package manager's own words.
// Railpack builds an Nx app itself, given its name.

// WorkspacePlan is how one app of a JavaScript workspace is built from the
// repository's root.
type WorkspacePlan struct {
	// BuildCommand builds the app and the workspace packages it uses.
	BuildCommand string
	// StartCommand starts it; empty when Railpack serves it as a static site
	// or starts it itself.
	StartCommand string
	// BuildEnv are build variables, KEY=VALUE: where a static site's files
	// are written, or the Nx app to build.
	BuildEnv []string
	// WatchPaths are the app's folder and those of the packages it uses.
	WatchPaths []string
	// Uses names the workspace packages it uses, for the words.
	Uses []string
}

type packageJSON struct {
	Name            string            `json:"name"`
	Scripts         map[string]string `json:"scripts"`
	Dependencies    map[string]string `json:"dependencies"`
	DevDependencies map[string]string `json:"devDependencies"`
	PackageManager  string            `json:"packageManager"`
}

func readPackageJSON(r repoView, p string) (packageJSON, bool) {
	var pkg packageJSON
	if !r.has(p) || json.Unmarshal([]byte(r.read(p)), &pkg) != nil {
		return pkg, false
	}
	return pkg, true
}

func (p packageJSON) uses(dep string) bool {
	_, a := p.Dependencies[dep]
	_, b := p.DevDependencies[dep]
	return a || b
}

// packageManager is the one the repository installs with: its packageManager
// field, else its lockfile. "yarn" is Yarn 1; "berry" is Yarn 2 and later.
func packageManager(r repoView, root packageJSON) string {
	if pm, _, _ := strings.Cut(root.PackageManager, "@"); pm != "" {
		if pm == "yarn" && !strings.HasPrefix(strings.TrimPrefix(root.PackageManager, "yarn@"), "1.") {
			return "berry"
		}
		return pm
	}
	switch {
	case r.has("pnpm-lock.yaml"):
		return "pnpm"
	case r.has("bun.lock") || r.has("bun.lockb"):
		return "bun"
	case r.has("yarn.lock") && r.has(".yarnrc.yml"):
		return "berry"
	case r.has("yarn.lock"):
		return "yarn"
	}
	return "npm"
}

// workspacePackages are the workspace's packages by name, each with its
// folder: every package.json below the root that names itself.
func workspacePackages(r repoView) map[string]string {
	out := map[string]string{}
	for p := range r.sizes {
		if path.Base(p) != "package.json" || !strings.Contains(p, "/") || skipped(p) {
			continue
		}
		if pkg, ok := readPackageJSON(r, p); ok && pkg.Name != "" {
			out[pkg.Name] = path.Dir(p)
		}
	}
	return out
}

// spaOutput is where a static site framework writes its files, relative to
// its folder, or "" for an app that is not one.
func spaOutput(app packageJSON) string {
	switch {
	case app.uses("react-scripts"):
		return "build"
	case app.uses("vite") || app.uses("astro"):
		if app.uses("@remix-run/dev") || app.uses("@react-router/dev") || app.uses("@sveltejs/kit") {
			return "" // a server app built with Vite
		}
		return "dist"
	}
	return ""
}

// workspacePlan is how the app in folder builds from the root, or nil when
// it needs nothing from outside its folder (and builds there as any app does)
// or is not a JavaScript workspace's.
func workspacePlan(r repoView, folder string) *WorkspacePlan {
	if folder == "." || workspaceTool(r) == "" {
		return nil
	}
	app, ok := readPackageJSON(r, folder+"/package.json")
	if !ok {
		return nil
	}
	root, _ := readPackageJSON(r, "package.json")
	pkgs := workspacePackages(r)

	plan := &WorkspacePlan{WatchPaths: []string{folder}}
	deps := map[string]string{}
	for k, v := range app.Dependencies {
		deps[k] = v
	}
	for k, v := range app.DevDependencies {
		deps[k] = v
	}
	for dep, version := range deps {
		dir, inWorkspace := pkgs[dep]
		if !inWorkspace && !strings.HasPrefix(version, "workspace:") {
			continue
		}
		plan.Uses = append(plan.Uses, dep)
		if inWorkspace && dir != folder {
			plan.WatchPaths = append(plan.WatchPaths, dir)
		}
	}
	if len(plan.Uses) == 0 {
		return nil
	}
	sort.Strings(plan.Uses)
	sort.Strings(plan.WatchPaths[1:])

	if r.has("nx.json") {
		name := app.Name
		if name == "" {
			name = path.Base(folder)
		}
		plan.BuildEnv = append(plan.BuildEnv, "RAILPACK_NX_APP="+name)
		return plan
	}

	pm := packageManager(r, root)
	// What the package manager calls the app: its name, or for pnpm its
	// folder, which pnpm takes as ./apps/web.
	sel := app.Name
	if sel == "" {
		if pm != "pnpm" && pm != "npm" {
			return nil // nothing to name it by
		}
		sel = "./" + folder
	}
	if app.Scripts["build"] != "" {
		turbo := (r.has("turbo.json") || r.has("turbo.jsonc")) && root.uses("turbo")
		switch {
		case turbo:
			run := map[string]string{"pnpm": "pnpm exec turbo", "bun": "bunx turbo", "yarn": "yarn turbo", "berry": "yarn turbo"}[pm]
			if run == "" {
				run = "npx turbo"
			}
			plan.BuildCommand = fmt.Sprintf("%s run build --filter=%s", run, sel)
		case pm == "pnpm":
			// The trailing ... is the app and every workspace package it uses.
			plan.BuildCommand = fmt.Sprintf("pnpm --filter %q run build", sel+"...")
		case pm == "berry":
			plan.BuildCommand = fmt.Sprintf("yarn workspaces foreach -Rt --from %q run build", sel)
		case pm == "yarn":
			// Yarn 1 cannot build a package's dependencies first; the
			// app builds alone, and packages it uses must need no build.
			plan.BuildCommand = fmt.Sprintf("yarn workspace %s run build", sel)
		case pm == "bun":
			plan.BuildCommand = fmt.Sprintf("bun run --filter %q build", sel)
		default:
			// npm cannot build one package's dependencies alone: every
			// package that has a build builds, in workspace order.
			plan.BuildCommand = "npm run build --workspaces --if-present"
		}
	}
	switch {
	case app.Scripts["start"] != "":
		switch pm {
		case "pnpm":
			plan.StartCommand = fmt.Sprintf("pnpm --filter %q run start", sel)
		case "yarn", "berry":
			plan.StartCommand = fmt.Sprintf("yarn workspace %s run start", sel)
		case "bun":
			plan.StartCommand = fmt.Sprintf("bun run --filter %q start", sel)
		default:
			plan.StartCommand = "npm run start --workspace " + folder
		}
	case spaOutput(app) != "":
		plan.BuildEnv = append(plan.BuildEnv, "RAILPACK_SPA_OUTPUT_DIR="+folder+"/"+spaOutput(app))
	}
	return plan
}

// applyWorkspacePlan turns a detection of the app in folder into one built
// from the root, when the app uses its workspace's packages and Railpack
// builds it. A Dockerfile that starts the app says how it builds itself.
func applyWorkspacePlan(d *Detection, r repoView, folder string) {
	if d.Builder == "dockerfile" {
		return
	}
	plan := workspacePlan(r, folder)
	if plan == nil {
		return
	}
	d.WorkspaceBuild = true
	d.BuildCommand = plan.BuildCommand
	switch {
	case plan.StartCommand != "":
		d.StartCommand = plan.StartCommand
	case strings.Contains(strings.Join(plan.BuildEnv, "\n"), "RAILPACK_SPA_OUTPUT_DIR="):
		d.StartCommand = "" // Railpack serves the built files itself
	}
	d.BuildEnvVars = strings.Join(plan.BuildEnv, "\n")
	d.WatchPaths = plan.WatchPaths
	d.Notes = append(d.Notes, fmt.Sprintf("Uses the workspace's own %s, so it builds from the repository's root, narrowed to this app.", joinNames(plan.Uses)))
}

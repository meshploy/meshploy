package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A pnpm workspace: web is a Vite site using @acme/ui, api a server using
// @acme/db. Each builds from the root, narrowed to itself and what it uses.
func pnpmShop() map[string]string {
	return map[string]string{
		"package.json":             `{"name": "shop", "private": true}`,
		"pnpm-workspace.yaml":      "packages:\n  - apps/*\n  - packages/*\n",
		"pnpm-lock.yaml":           "",
		"apps/web/package.json":    `{"name": "@acme/web", "scripts": {"build": "vite build"}, "dependencies": {"@acme/ui": "workspace:*"}, "devDependencies": {"vite": "^6"}}`,
		"apps/api/package.json":    `{"name": "@acme/api", "scripts": {"build": "tsc", "start": "node dist/index.js"}, "dependencies": {"@acme/db": "workspace:^"}}`,
		"apps/docs/package.json":   `{"name": "@acme/docs", "scripts": {"start": "node serve.js"}, "dependencies": {"express": "^5"}}`,
		"packages/ui/package.json": `{"name": "@acme/ui", "scripts": {"build": "tsc"}}`,
		"packages/db/package.json": `{"name": "@acme/db"}`,
	}
}

func TestWorkspaceBuildWithPnpm(t *testing.T) {
	r := view(pnpmShop())

	web := workspacePlan(r, "apps/web")
	require.NotNil(t, web)
	assert.Equal(t, `pnpm --filter "@acme/web..." run build`, web.BuildCommand)
	assert.Empty(t, web.StartCommand)
	assert.Equal(t, []string{"RAILPACK_SPA_OUTPUT_DIR=apps/web/dist"}, web.BuildEnv)
	assert.Equal(t, []string{"apps/web", "packages/ui"}, web.WatchPaths)

	api := workspacePlan(r, "apps/api")
	require.NotNil(t, api)
	assert.Equal(t, `pnpm --filter "@acme/api" run start`, api.StartCommand)
	assert.Empty(t, api.BuildEnv)
	assert.Equal(t, []string{"@acme/db"}, api.Uses)

	// Using nothing of the workspace's, an app builds in its folder as any app does.
	assert.Nil(t, workspacePlan(r, "apps/docs"))
	assert.Nil(t, workspacePlan(r, "."))
}

// The detection says so, and drops a start command Railpack's static site
// server replaces.
func TestDetectionOfAWorkspaceApp(t *testing.T) {
	r := view(pnpmShop())
	d := suggest(detectStack(subView(r, "apps/web"), ""))
	d.Folder = "apps/web"
	applyWorkspacePlan(&d, r, "apps/web")
	assert.True(t, d.WorkspaceBuild)
	assert.Equal(t, "RAILPACK_SPA_OUTPUT_DIR=apps/web/dist", d.BuildEnvVars)
	assert.Empty(t, d.StartCommand)
	assert.Contains(t, d.Notes, "Uses the workspace's own @acme/ui, so it builds from the repository's root, narrowed to this app.")
}

func TestWorkspaceBuildByTool(t *testing.T) {
	app := `{"name": "web", "scripts": {"build": "next build", "start": "next start"}, "dependencies": {"ui": "*", "next": "15"}}`
	base := func(extra map[string]string) repoView {
		files := map[string]string{
			"package.json":             `{"workspaces": ["apps/*", "packages/*"], "devDependencies": {}}`,
			"apps/web/package.json":    app,
			"packages/ui/package.json": `{"name": "ui"}`,
		}
		for k, v := range extra {
			files[k] = v
		}
		return view(files)
	}

	npm := workspacePlan(base(map[string]string{"package-lock.json": "{}"}), "apps/web")
	assert.Equal(t, "npm run build --workspaces --if-present", npm.BuildCommand)
	assert.Equal(t, "npm run start --workspace apps/web", npm.StartCommand)

	yarn1 := workspacePlan(base(map[string]string{"yarn.lock": ""}), "apps/web")
	assert.Equal(t, "yarn workspace web run build", yarn1.BuildCommand)
	assert.Equal(t, "yarn workspace web run start", yarn1.StartCommand)

	berry := workspacePlan(base(map[string]string{"yarn.lock": "", ".yarnrc.yml": ""}), "apps/web")
	assert.Equal(t, `yarn workspaces foreach -Rt --from "web" run build`, berry.BuildCommand)

	bun := workspacePlan(base(map[string]string{"bun.lock": ""}), "apps/web")
	assert.Equal(t, `bun run --filter "web" build`, bun.BuildCommand)

	turbo := workspacePlan(base(map[string]string{
		"package.json": `{"workspaces": ["apps/*", "packages/*"], "devDependencies": {"turbo": "^2"}}`,
		"turbo.json":   "{}", "package-lock.json": "{}",
	}), "apps/web")
	assert.Equal(t, "npx turbo run build --filter=web", turbo.BuildCommand)
	assert.Equal(t, "npm run start --workspace apps/web", turbo.StartCommand)

	// Railpack builds an Nx app itself, by its name.
	nx := workspacePlan(base(map[string]string{"nx.json": "{}"}), "apps/web")
	assert.Equal(t, []string{"RAILPACK_NX_APP=web"}, nx.BuildEnv)
	assert.Empty(t, nx.BuildCommand)
	assert.Empty(t, nx.StartCommand)

	// The packageManager field outranks the lockfile.
	pm := workspacePlan(base(map[string]string{
		"package.json": `{"workspaces": ["apps/*", "packages/*"], "packageManager": "pnpm@9.1.0"}`, "package-lock.json": "{}",
	}), "apps/web")
	assert.Equal(t, `pnpm --filter "web..." run build`, pm.BuildCommand)
}

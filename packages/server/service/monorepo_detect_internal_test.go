package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func layout(files map[string]string, links ...string) Detection {
	r := view(files)
	r.links = links
	d := suggest(detectStack(r, ""))
	detectLayout(&d, r, ".")
	return d
}

// A Turborepo with two apps and a shared package: the apps are offered, the
// package is not, and the form is asked to choose.
func TestDetectFindsAMonoreposApps(t *testing.T) {
	d := layout(map[string]string{
		"package.json":             `{"private": true, "workspaces": ["apps/*", "packages/*"]}`,
		"turbo.json":               `{}`,
		"apps/web/package.json":    `{"scripts": {"build": "vite build"}}`,
		"apps/web/src/main.tsx":    "",
		"apps/api/go.mod":          "module api",
		"apps/api/cmd/x/go.mod":    "module nested",
		"packages/ui/package.json": `{"name": "@acme/ui"}`,
		"docs/package.json":        `{}`,
	})
	assert.Equal(t, "npm workspaces", d.Workspace)
	require.Len(t, d.Apps, 2)
	assert.Equal(t, FoundApp{Folder: "apps/api", Summary: "Go"}, d.Apps[0])
	assert.Equal(t, FoundApp{Folder: "apps/web", Summary: "Node.js"}, d.Apps[1])
	assert.Contains(t, d.Notes, "This repository holds 2 apps. Choose the folder of the one this service runs.")
}

// A package under packages/ that has a Dockerfile runs, so it is an app.
func TestDetectCountsALibraryFolderThatRuns(t *testing.T) {
	d := layout(map[string]string{
		"pnpm-workspace.yaml":          "",
		"packages/worker/package.json": `{}`,
		"packages/worker/Dockerfile":   "FROM node",
		"packages/ui/package.json":     `{}`,
	})
	assert.Equal(t, "pnpm workspaces", d.Workspace)
	require.Len(t, d.Apps, 1)
	assert.Equal(t, "packages/worker", d.Apps[0].Folder)
}

// The deploy this exists for: the app was made inside the repository with its
// own .git, so the repository holds only a pointer to it, and a root
// package.json that starts nothing.
func TestDetectSaysWhenAnAppsFilesAreNotInTheRepository(t *testing.T) {
	d := layout(map[string]string{
		"package.json":      `{"dependencies": {"gh-pages": "^6"}}`,
		"package-lock.json": "{}",
		"README.md":         "",
	}, "first_app")
	assert.Empty(t, d.Apps)
	assert.Equal(t, []string{"first_app"}, d.Unfetched)
	require.NotEmpty(t, d.Notes)
	assert.Contains(t, d.Notes[len(d.Notes)-1], "first_app holds a repository of its own that was never added")
}

// A declared submodule is told apart: it is meant to be there, but builds do
// not fetch it.
func TestDetectNamesASubmodule(t *testing.T) {
	d := layout(map[string]string{
		"go.mod":      "module x",
		".gitmodules": "[submodule \"vendor/lib\"]\n\tpath = vendor/lib\n\turl = https://example.com/lib\n",
	}, "vendor/lib")
	assert.Equal(t, []string{"vendor/lib"}, d.Unfetched)
	assert.Contains(t, d.Notes[len(d.Notes)-1], "vendor/lib is a git submodule, which builds do not fetch")
}

// An app at the root with folders of tooling beside it asks nothing.
func TestDetectLeavesAnAppAtTheRootAlone(t *testing.T) {
	d := layout(map[string]string{
		"package.json":         `{"scripts": {"start": "node server.js"}}`,
		"server.js":            "",
		"scripts/package.json": `{}`,
		"e2e/package.json":     `{}`,
	})
	assert.Empty(t, d.Apps)
	assert.Empty(t, d.Workspace)
	assert.Empty(t, d.Unfetched)
	assert.Empty(t, d.Notes)
}

// One app kept in a folder, with nothing that runs at the root.
func TestDetectFindsTheOneAppInAFolder(t *testing.T) {
	files := map[string]string{
		"README.md":             "",
		"frontend/package.json": `{"scripts": {"dev": "vite", "build": "vite build"}}`,
	}
	d := layout(files)
	require.Len(t, d.Apps, 1)
	assert.Equal(t, "frontend", d.Apps[0].Folder)
	assert.Contains(t, d.Notes, "The app looks to be in frontend, not at the repository's root. Build from that folder.")

	// Once the folder is chosen, the folder's own app is described, and
	// nobody is asked to choose again.
	r := view(files)
	app := subView(r, "frontend")
	chosen := suggest(detectStack(app, ""))
	detectLayout(&chosen, r, "frontend")
	assert.Equal(t, "Node.js", chosen.Summary)
	assert.Len(t, chosen.Apps, 1)
	assert.Empty(t, chosen.Notes)
}

func TestBuildFolder(t *testing.T) {
	for in, want := range map[string]string{
		"": ".", ".": ".", " / ": ".", "apps/web": "apps/web", "/apps/web/": "apps/web", "apps//web/./": "apps/web",
	} {
		got, err := BuildFolder(in)
		require.NoError(t, err, in)
		assert.Equal(t, want, got, in)
	}
	for _, bad := range []string{"..", "../x", "apps/../../x", "-x", "a\\b", "a\nb"} {
		_, err := BuildFolder(bad)
		assert.ErrorIs(t, err, ErrBuildFolder, bad)
	}
}

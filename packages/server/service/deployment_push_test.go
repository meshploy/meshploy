package service

import "testing"

// The push URL belongs to an integration, so what decides which services build
// is the repository and branch in the payload. The two sides are written by
// different hands - a picker on one, a person typing a URL on the other - and
// when they failed to match, auto-deploy did nothing and said nothing.
func TestRepositoriesMatchHoweverTheyWereWritten(t *testing.T) {
	same := [][2]string{
		{"owner/app", "owner/app"},
		{"owner/app", "https://github.com/owner/app"},
		{"owner/app", "https://github.com/owner/app.git"},
		// No ssh form here on purpose: a clone is https, so an ssh remote in
		// git_repo would not build even if a push matched it.
		{"owner/app", "/owner/app/"},
		{"Owner/App", "owner/app"},
		{"group/sub/app", "https://gitlab.com/group/sub/app.git"},
		{"workspace/app", "https://bitbucket.org/workspace/app"},
	}
	for _, c := range same {
		if normalizeRepoPath(c[0]) != normalizeRepoPath(c[1]) {
			t.Errorf("%q and %q name the same repository, got %q and %q",
				c[0], c[1], normalizeRepoPath(c[0]), normalizeRepoPath(c[1]))
		}
	}

	// Different repositories must stay different, including a subgroup that
	// merely ends with the same name.
	differ := [][2]string{
		{"owner/app", "owner/other"},
		{"group/sub/app", "group/app"},
		{"owner/app", "other/app"},
	}
	for _, c := range differ {
		if normalizeRepoPath(c[0]) == normalizeRepoPath(c[1]) {
			t.Errorf("%q and %q are different repositories, both read as %q",
				c[0], c[1], normalizeRepoPath(c[0]))
		}
	}
}

// In a monorepo, a push that touches the docs should not rebuild three
// services. Watched paths say which part of the repository a service is built
// from.
func TestWatchedPathsDecideWhetherAPushBuilds(t *testing.T) {
	cases := []struct {
		name    string
		watch   []string
		changed []string
		build   bool
	}{
		{"no paths watched builds on anything", nil, []string{"docs/readme.md"}, true},
		{"a touched folder builds", []string{"apps/api"}, []string{"apps/api/main.go"}, true},
		{"an untouched folder does not", []string{"apps/api"}, []string{"apps/web/index.html"}, false},
		{"the folder itself counts", []string{"apps/api"}, []string{"apps/api"}, true},
		{"a prefix that is not a path boundary does not", []string{"apps/api"}, []string{"apps/api-docs/x.md"}, false},
		{"any one of several paths is enough", []string{"apps/api", "packages/db"}, []string{"packages/db/models.go"}, true},
		{"leading slashes are noise", []string{"/apps/api/"}, []string{"/apps/api/main.go"}, true},
		{"a watched file builds when it changes", []string{"go.mod"}, []string{"go.mod"}, true},
		// Bitbucket sends no file list, and GitHub truncates a large push.
		// Treating "cannot tell" as "nothing changed" would quietly stop
		// deploying the moment someone merged a big branch.
		{"an unknown file list builds", []string{"apps/api"}, nil, true},
	}
	for _, c := range cases {
		if got := pushTouches(c.watch, c.changed); got != c.build {
			t.Errorf("%s: got %v, want %v", c.name, got, c.build)
		}
	}
}

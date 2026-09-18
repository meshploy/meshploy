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

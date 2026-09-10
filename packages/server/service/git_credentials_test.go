package service

import (
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/internal/gittest"
)

func TestRepoCloneURL(t *testing.T) {
	cases := []struct{ base, repo, want string }{
		{"https://github.com", "owner/repo", "https://github.com/owner/repo.git"},
		{"https://gitlab.example.com/", "group/sub/project", "https://gitlab.example.com/group/sub/project.git"},
		{"https://github.com", " owner/repo.git ", "https://github.com/owner/repo.git"},
		// A public repository typed into the console is stored as a full URL.
		{"https://github.com", "https://github.com/org/repo", "https://github.com/org/repo"},
		{"https://github.com", "https://gitlab.com/org/repo/", "https://gitlab.com/org/repo"},
	}
	for _, c := range cases {
		if got := repoCloneURL(c.base, c.repo); got != c.want {
			t.Errorf("repoCloneURL(%q, %q) = %q, want %q", c.base, c.repo, got, c.want)
		}
	}
}

// The case this exists for: a GitLab integration used to fail every deploy
// with a GitHub App error before the build started.
func TestCloneCredentialsPerProvider(t *testing.T) {
	s := &GitIntegrationService{}
	later := time.Now().Add(time.Hour)
	cases := []struct {
		name        string
		integration *db.GitIntegration
		repo        string
		want        gitCredentials
	}{
		{"public repository", nil, "https://github.com/org/public",
			gitCredentials{URL: "https://github.com/org/public"}},
		{"GitLab personal access token", &db.GitIntegration{Provider: "gitlab", AuthMethod: "pat", InstallationID: "glpat-abc"},
			"group/project", gitCredentials{URL: "https://gitlab.com/group/project.git", User: "oauth2", Token: "glpat-abc"}},
		{"self-hosted GitLab OAuth, token still valid", &db.GitIntegration{Provider: "gitlab", AuthMethod: "oauth",
			BaseURL: "https://git.example.com/", InstallationID: "oauth-tok", OAuthTokenExpiry: &later},
			"team/sub/app", gitCredentials{URL: "https://git.example.com/team/sub/app.git", User: "oauth2", Token: "oauth-tok"}},
		{"Gitea personal access token", &db.GitIntegration{Provider: "gitea", AuthMethod: "pat", BaseURL: "https://gitea.example.com", InstallationID: "gt-abc"},
			"owner/repo", gitCredentials{URL: "https://gitea.example.com/owner/repo.git", User: "oauth2", Token: "gt-abc"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := s.cloneCredentials(t.Context(), c.integration, c.repo)
			if err != nil || got != c.want {
				t.Fatalf("got %+v, %v; want %+v", got, err, c.want)
			}
		})
	}
}

func TestCloneCredentialsRefusals(t *testing.T) {
	s := &GitIntegrationService{}
	past := time.Now().Add(-time.Hour)

	// An OAuth token that has expired with nothing to renew it from.
	_, err := s.cloneCredentials(t.Context(), &db.GitIntegration{Provider: "gitlab", AuthMethod: "oauth",
		InstallationID: "old", OAuthTokenExpiry: &past}, "group/project")
	if !errors.Is(err, errGitTokenExpired) {
		t.Errorf("expired GitLab token: got %v", err)
	}

	// A GitHub App that was never installed is still a GitHub-only error.
	_, err = s.cloneCredentials(t.Context(), &db.GitIntegration{Provider: "github", AuthMethod: "app"}, "owner/repo")
	if !errors.Is(err, errGitHubAppIncomplete) {
		t.Errorf("uninstalled GitHub App: got %v", err)
	}
}

func TestGitEnv(t *testing.T) {
	if env := (gitCredentials{URL: "https://github.com/org/public"}).gitEnv(); len(env) != 1 || env[0] != "GIT_TERMINAL_PROMPT=0" {
		t.Errorf("anonymous clone env = %v, want no credentials", env)
	}
	env := strings.Join(gitCredentials{User: "oauth2", Token: "tok"}.gitEnv(), "\n")
	if want := "GIT_CONFIG_VALUE_0=Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte("oauth2:tok")); !strings.Contains(env, want) {
		t.Errorf("env = %q, want %q", env, want)
	}
}

// A real clone over HTTP: the token must reach the server as a header, and
// must not be left in the clone, where a build could copy it into an image.
func TestCloneRepoSendsTheTokenAsAHeaderOnly(t *testing.T) {
	url, seen := gittest.Serve(t)
	const token = "s3cret-token"

	dir, err := cloneRepo(t.Context(), gitCredentials{URL: url, User: "oauth2", Token: token}, "main")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })

	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("oauth2:"+token))
	for i, h := range seen() {
		if h != want {
			t.Errorf("request %d sent Authorization %q, want %q", i, h, want)
		}
	}
	cfg, err := os.ReadFile(filepath.Join(dir, ".git", "config"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(cfg), token) {
		t.Errorf("the token was written into .git/config:\n%s", cfg)
	}
	if _, err := os.Stat(filepath.Join(dir, "README")); err != nil {
		t.Errorf("clone has no README: %v", err)
	}

	// A public repository: no header at all.
	before := len(seen())
	dir2, err := cloneRepo(t.Context(), gitCredentials{URL: url}, "main")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir2) })
	for _, h := range seen()[before:] {
		if h != "" {
			t.Errorf("anonymous clone sent Authorization %q", h)
		}
	}
}

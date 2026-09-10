package service

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/meshploy/packages/db"
)

// gitCredentials is how a clone reaches a repository: its https URL, and the
// user and token to present.
//
// The token is never put into the URL. git writes the remote URL into the
// clone's .git/config, so a build that copies its context (a Dockerfile's
// COPY . .) would carry a token written there into the image, and a GitLab or
// Gitea token lives far longer than an hour.
type gitCredentials struct {
	URL   string // https://host/path.git, without credentials
	User  string // presented with Token: x-access-token (GitHub), oauth2 (GitLab, Gitea)
	Token string // empty for a public repository
}

var (
	errGitHubAppIncomplete = errors.New("GitHub App not fully configured; complete setup and installation in Integrations")
	errGitTokenExpired     = errors.New("the git integration's access token has expired and could not be renewed; reconnect it in Integrations")
)

// cloneCredentials resolves how to clone repo. integration is nil for a public
// repository, which is cloned anonymously.
//
// Every provider is handled here, so builds and stack syncs cannot drift apart.
// Deploys used to assume a GitHub App for any integration, which failed every
// GitLab and Gitea build before it started.
func (s *GitIntegrationService) cloneCredentials(ctx context.Context, integration *db.GitIntegration, repo string) (gitCredentials, error) {
	if integration == nil {
		return gitCredentials{URL: repoCloneURL("https://github.com", repo)}, nil
	}

	switch integration.Provider {
	case "gitlab", "gitea":
		// A personal access token comes back as stored; an OAuth token is
		// renewed first when it is about to expire (GitLab's last two hours).
		token, err := s.resolveOAuthToken(ctx, integration, false)
		if errors.Is(err, errUnauthorized) {
			return gitCredentials{}, errGitTokenExpired
		}
		if err != nil {
			return gitCredentials{}, err
		}
		base := strings.TrimRight(integration.BaseURL, "/")
		if integration.Provider == "gitlab" {
			base = gitLabBase(integration.BaseURL)
		}
		// Both take the token as the password under any user name; oauth2 is
		// the one GitLab documents.
		return gitCredentials{URL: repoCloneURL(base, repo), User: "oauth2", Token: token}, nil

	default: // GitHub, which Meshploy connects only through a GitHub App
		if integration.GHAppID == "" || string(integration.InstallationID) == "" {
			return gitCredentials{}, errGitHubAppIncomplete
		}
		token, err := getInstallationToken(integration.GHAppID, string(integration.GHPrivateKey), string(integration.InstallationID))
		if err != nil {
			return gitCredentials{}, fmt.Errorf("failed to get GitHub token: %w", err)
		}
		return gitCredentials{URL: repoCloneURL("https://github.com", repo), User: "x-access-token", Token: token}, nil
	}
}

// repoCloneURL returns the https clone URL for repo. That is either a full URL,
// which is how the console stores a public repository typed in by hand, or a
// path on the provider's host from the repository picker: owner/repo, or a
// GitLab group/subgroup/project path.
func repoCloneURL(base, repo string) string {
	repo = strings.TrimSpace(repo)
	if strings.HasPrefix(repo, "https://") || strings.HasPrefix(repo, "http://") {
		return strings.TrimRight(repo, "/")
	}
	path := strings.TrimSuffix(strings.Trim(repo, "/"), ".git")
	return strings.TrimRight(base, "/") + "/" + path + ".git"
}

// gitEnv is the environment that has git present the credentials, as a header
// on this one command. It goes through the environment rather than -c, so the
// token is not in the process's arguments either.
func (c gitCredentials) gitEnv() []string {
	env := []string{"GIT_TERMINAL_PROMPT=0"}
	if c.Token != "" {
		basic := base64.StdEncoding.EncodeToString([]byte(c.User + ":" + c.Token))
		env = append(env,
			"GIT_CONFIG_COUNT=1",
			"GIT_CONFIG_KEY_0=http.extraHeader",
			"GIT_CONFIG_VALUE_0=Authorization: Basic "+basic,
		)
	}
	return env
}

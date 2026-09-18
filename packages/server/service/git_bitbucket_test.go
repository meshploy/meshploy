package service

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// stubBitbucket points the Bitbucket helpers at a test server for the length of
// the test.
func stubBitbucket(t *testing.T, h http.HandlerFunc) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	api, web := bitbucketAPI, bitbucketWeb
	bitbucketAPI, bitbucketWeb = srv.URL+"/2.0", srv.URL
	t.Cleanup(func() { bitbucketAPI, bitbucketWeb = api, web })
}

// Bitbucket pages by handing back the next page's whole URL, which is the one
// thing a reader written against GitLab's or Gitea's page numbers gets wrong:
// it would stop after the first page and quietly show a truncated list.
func TestBitbucketReposFollowEveryPage(t *testing.T) {
	var gotAuth string
	stubBitbucket(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.URL.Query().Get("page") == "2" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"values": []map[string]any{
					{"full_name": "ws/second", "is_private": false, "mainbranch": map[string]string{"name": "trunk"}},
				},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"values": []map[string]any{
				{"full_name": "ws/first", "is_private": true, "mainbranch": map[string]string{"name": "main"}},
			},
			"next": "http://" + r.Host + r.URL.Path + "?page=2",
		})
	})

	repos, err := listBitbucketRepos("tok", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 2 {
		t.Fatalf("want both pages, got %d: %+v", len(repos), repos)
	}
	if repos[0].FullName != "ws/first" || repos[0].DefaultBranch != "main" || !repos[0].Private {
		t.Errorf("first page read wrong: %+v", repos[0])
	}
	if repos[1].FullName != "ws/second" || repos[1].DefaultBranch != "trunk" {
		t.Errorf("second page read wrong: %+v", repos[1])
	}
	// An Atlassian API token is a bearer, not basic auth with an account.
	if gotAuth != "Bearer tok" {
		t.Errorf("want a bearer token, got %q", gotAuth)
	}
}

// A workspace narrows the listing, the way a group does on GitLab.
func TestBitbucketWorkspaceNarrowsTheListing(t *testing.T) {
	var path string
	stubBitbucket(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		_, _ = w.Write([]byte(`{"values":[]}`))
	})
	if _, err := listBitbucketRepos("tok", "my-workspace"); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(path, "/repositories/my-workspace") {
		t.Errorf("want the workspace in the path, got %q", path)
	}
}

func TestBitbucketBranchesAreListed(t *testing.T) {
	stubBitbucket(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/repositories/ws/app/refs/branches") {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		_, _ = fmt.Fprint(w, `{"values":[{"name":"main"},{"name":"staging"}]}`)
	})
	branches, err := listBitbucketBranches("tok", "ws/app")
	if err != nil {
		t.Fatal(err)
	}
	if len(branches) != 2 || branches[0] != "main" || branches[1] != "staging" {
		t.Errorf("got %+v", branches)
	}
}

// A rejected token has to come back as errUnauthorized, because that is what
// makes the caller refresh once and retry, and then say "reconnect".
func TestBitbucketRejectedTokenIsUnauthorized(t *testing.T) {
	stubBitbucket(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	if _, err := listBitbucketRepos("stale", ""); err != errUnauthorized {
		t.Fatalf("want errUnauthorized, got %v", err)
	}
}

// The consumer's scopes are fixed when it is created, so asking for scopes in
// the authorize URL is wrong here even though GitLab and Gitea want it.
func TestBitbucketAuthorizeURLAsksForNoScopes(t *testing.T) {
	u := bitbucketAuthorizeURL("client", "https://meshploy.example/api/v1/bitbucket/callback", "state123")
	if strings.Contains(u, "scope=") {
		t.Errorf("no scope parameter belongs here: %s", u)
	}
	for _, want := range []string{"/site/oauth2/authorize", "client_id=client", "response_type=code", "state=state123",
		"redirect_uri=https%3A%2F%2Fmeshploy.example%2Fapi%2Fv1%2Fbitbucket%2Fcallback"} {
		if !strings.Contains(u, want) {
			t.Errorf("missing %q in %s", want, u)
		}
	}
}

// A GitLab project is addressed by its full path, encoded slashes and all;
// sending it raw asks for a project literally called "group/sub/app".
func TestHookURLsAddressTheRepositoryTheProviderWay(t *testing.T) {
	if got := gitLabHooksURL("", "group/sub/app"); got != "https://gitlab.com/api/v4/projects/group%2Fsub%2Fapp/hooks" {
		t.Errorf("gitlab: %s", got)
	}
	if got := giteaHooksURL("https://codeberg.org/", "team/app"); got != "https://codeberg.org/api/v1/repos/team/app/hooks" {
		t.Errorf("gitea: %s", got)
	}
	if got := bitbucketHooksURL("ws/app"); got != bitbucketAPI+"/repositories/ws/app/hooks" {
		t.Errorf("bitbucket: %s", got)
	}
}

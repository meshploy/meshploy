package handler

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"
)

// A delivery is a request to build and deploy whatever is in a repository, from
// an endpoint that cannot be authenticated any other way. These tests are about
// the two things that matter: an unsigned or wrongly signed delivery is
// refused, and a genuine one is read correctly.

const testSecret = "a-secret-that-only-the-provider-and-we-know"

func sign(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func deliver(provider string, body string, headers map[string]string) (gitPushEvent, bool) {
	r := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/git/"+provider+"/x", nil)
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	return parseGitPush(provider, r, []byte(body), testSecret)
}

func TestGitLabPushIsTakenOnItsToken(t *testing.T) {
	body := `{"ref":"refs/heads/main","project":{"path_with_namespace":"group/sub/app"}}`

	if _, ok := deliver("gitlab", body, map[string]string{
		"X-Gitlab-Token": "not-the-secret", "X-Gitlab-Event": "Push Hook",
	}); ok {
		t.Error("a delivery with the wrong token must be refused")
	}
	if _, ok := deliver("gitlab", body, map[string]string{"X-Gitlab-Event": "Push Hook"}); ok {
		t.Error("a delivery with no token must be refused")
	}

	event, ok := deliver("gitlab", body, map[string]string{
		"X-Gitlab-Token": testSecret, "X-Gitlab-Event": "Push Hook",
	})
	if !ok {
		t.Fatal("a delivery with the right token must be accepted")
	}
	// The whole path, subgroups included: that is what the repository picker
	// stores and what the deploy matcher compares against.
	if len(event.refs) != 1 || event.refs[0] != (gitPushRef{"group/sub/app", "main"}) {
		t.Errorf("got %+v", event.refs)
	}
}

func TestGiteaAndForgejoPushAreVerifiedByTheirSignature(t *testing.T) {
	body := `{"ref":"refs/heads/release","repository":{"full_name":"team/app"}}`
	good := sign([]byte(body), testSecret)

	if _, ok := deliver("gitea", body, map[string]string{
		"X-Gitea-Signature": sign([]byte(body), "another-secret"), "X-Gitea-Event": "push",
	}); ok {
		t.Error("a delivery signed with the wrong secret must be refused")
	}
	if _, ok := deliver("gitea", body, map[string]string{"X-Gitea-Event": "push"}); ok {
		t.Error("an unsigned delivery must be refused")
	}

	event, ok := deliver("gitea", body, map[string]string{
		"X-Gitea-Signature": good, "X-Gitea-Event": "push",
	})
	if !ok || len(event.refs) != 1 || event.refs[0] != (gitPushRef{"team/app", "release"}) {
		t.Fatalf("gitea: ok=%v refs=%+v", ok, event.refs)
	}

	// Codeberg runs Forgejo, which sends the same delivery under its own header
	// names. It must be taken on those alone.
	event, ok = deliver("gitea", body, map[string]string{
		"X-Forgejo-Signature": good, "X-Forgejo-Event": "push",
	})
	if !ok || len(event.refs) != 1 || event.refs[0] != (gitPushRef{"team/app", "release"}) {
		t.Fatalf("forgejo: ok=%v refs=%+v", ok, event.refs)
	}
}

func TestBitbucketPushIsVerifiedByItsSignature(t *testing.T) {
	body := `{"repository":{"full_name":"workspace/app"},"push":{"changes":[
		{"new":{"type":"branch","name":"main"}},
		{"new":{"type":"tag","name":"v1.2.0"}},
		{"new":{"type":"branch","name":"staging"}}
	]}}`
	good := "sha256=" + sign([]byte(body), testSecret)

	if _, ok := deliver("bitbucket", body, map[string]string{
		"X-Hub-Signature": "sha256=" + sign([]byte(body), "another-secret"), "X-Event-Key": "repo:push",
	}); ok {
		t.Error("a delivery signed with the wrong secret must be refused")
	}

	event, ok := deliver("bitbucket", body, map[string]string{
		"X-Hub-Signature": good, "X-Event-Key": "repo:push",
	})
	if !ok {
		t.Fatal("a correctly signed delivery must be accepted")
	}
	// One delivery, several branches - and a tag is not a branch.
	if len(event.refs) != 2 ||
		event.refs[0] != (gitPushRef{"workspace/app", "main"}) ||
		event.refs[1] != (gitPushRef{"workspace/app", "staging"}) {
		t.Errorf("got %+v", event.refs)
	}
}

// A genuine delivery that is not a push - an issue, a tag, a comment - is
// accepted and does nothing. Refusing it would make the provider retry and
// eventually disable the hook.
func TestOtherEventsAreAcceptedAndIgnored(t *testing.T) {
	cases := []struct {
		provider string
		body     string
		headers  map[string]string
	}{
		{"gitlab", `{"object_kind":"issue"}`, map[string]string{
			"X-Gitlab-Token": testSecret, "X-Gitlab-Event": "Issue Hook"}},
		{"gitea", `{}`, map[string]string{
			"X-Gitea-Signature": sign([]byte(`{}`), testSecret), "X-Gitea-Event": "issues"}},
		{"bitbucket", `{}`, map[string]string{
			"X-Hub-Signature": "sha256=" + sign([]byte(`{}`), testSecret), "X-Event-Key": "issue:created"}},
	}
	for _, c := range cases {
		event, ok := deliver(c.provider, c.body, c.headers)
		if !ok {
			t.Errorf("%s: a genuine delivery must not be refused", c.provider)
		}
		if len(event.refs) != 0 {
			t.Errorf("%s: nothing should be deployed, got %+v", c.provider, event.refs)
		}
	}
}

// A tag push carries no branch, so nothing is deployed.
func TestTagPushesDeployNothing(t *testing.T) {
	body := `{"ref":"refs/tags/v1.0.0","repository":{"full_name":"team/app"}}`
	event, ok := deliver("gitea", body, map[string]string{
		"X-Gitea-Signature": sign([]byte(body), testSecret), "X-Gitea-Event": "push",
	})
	if !ok {
		t.Fatal("the delivery is genuine and must be accepted")
	}
	for _, ref := range event.refs {
		if ref.branch != "" {
			t.Errorf("a tag push has no branch, got %q", ref.branch)
		}
	}
}

// An unknown provider in the path must not be treated as trusted.
func TestUnknownProviderIsRefused(t *testing.T) {
	if _, ok := deliver("sourceforge", `{}`, nil); ok {
		t.Error("an unknown provider must be refused")
	}
}

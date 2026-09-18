package handler

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// Deploy on push, for every provider that is not a GitHub App.
//
// The machinery behind this was already provider-agnostic - a push finds every
// service tracking that integration, repo and branch - but the only way in was
// the GitHub App's own webhook. A GitLab or Gitea service could be marked
// "deploy on push" and nothing would ever arrive. This is that missing door,
// one shape for all of them:
//
//	POST /api/v1/webhooks/git/{provider}/{integrationId}
//
// GitHub keeps its own route, unchanged, because its URL is baked into every
// App manifest already created.
//
// Each provider differs only in how it proves the delivery is genuine and where
// it puts the repository and branch, which is all gitPushEvent covers.

// maxWebhookBody is what is read from a delivery before giving up. A push
// payload is kilobytes; a monorepo's first push with thousands of commits is
// still far under this.
const maxWebhookBody = 10 << 20 // 10 MiB

// GitWebhook handles a push from GitLab, Gitea/Forgejo or Bitbucket.
//
// It answers 200 to anything it recognises but cannot act on - an unknown
// integration, a tag push, an event that is not a push. A provider that gets an
// error retries for hours and eventually disables the hook, and none of those
// are failures worth that.
func (h *Handler) GitWebhook(w http.ResponseWriter, r *http.Request) {
	provider := strings.ToLower(chi.URLParam(r, "provider"))
	integrationID, err := uuid.Parse(chi.URLParam(r, "integrationId"))
	if err != nil {
		http.Error(w, "invalid integration ID", http.StatusBadRequest)
		return
	}

	integration, err := h.svc.GitIntegrations.GetByID(r.Context(), integrationID)
	if err != nil {
		// Stale URL in someone's repository settings: stop it retrying.
		w.WriteHeader(http.StatusOK)
		return
	}
	if !strings.EqualFold(integration.Provider, provider) {
		http.Error(w, "integration is not a "+provider+" integration", http.StatusBadRequest)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxWebhookBody))
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	secret := string(integration.WebhookSecret)
	if secret == "" {
		// Nothing to check the delivery against, so anyone who learned the URL
		// could start builds. Refuse until the hook is set up again.
		http.Error(w, "this integration has no webhook secret; reconnect it", http.StatusUnauthorized)
		return
	}

	event, ok := parseGitPush(provider, r, body, secret)
	if !ok {
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}
	for _, ref := range event.refs {
		if ref.repo == "" || ref.branch == "" {
			continue
		}
		h.svc.Deployments.FindAndTriggerForPush(r.Context(), integrationID, ref.repo, ref.branch, ref.changed)
	}
	w.WriteHeader(http.StatusOK)
}

// gitPushRef is one repository and branch that was pushed to, with the files
// the push touched where the provider says.
type gitPushRef struct {
	repo, branch string
	changed      []string
}

// gitPushEvent is what a delivery amounts to: the pushed branches. Bitbucket
// sends several in one delivery when several are pushed at once.
type gitPushEvent struct{ refs []gitPushRef }

// parseGitPush verifies the delivery and reads the branches out of it. The
// second return is false only when the delivery cannot be trusted; a push that
// is genuine but uninteresting comes back true with no refs.
func parseGitPush(provider string, r *http.Request, body []byte, secret string) (gitPushEvent, bool) {
	switch provider {
	case "gitlab":
		// GitLab sends the secret back as a plain header rather than signing
		// the body, so this is a comparison, not a verification - in constant
		// time, since it is a credential.
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Gitlab-Token")), []byte(secret)) != 1 {
			return gitPushEvent{}, false
		}
		if !strings.EqualFold(r.Header.Get("X-Gitlab-Event"), "Push Hook") {
			return gitPushEvent{}, true
		}
		var p struct {
			Ref     string `json:"ref"`
			Project struct {
				PathWithNamespace string `json:"path_with_namespace"`
			} `json:"project"`
			Commits []commitFiles `json:"commits"`
		}
		if err := json.Unmarshal(body, &p); err != nil {
			return gitPushEvent{}, true
		}
		return gitPushEvent{refs: []gitPushRef{{p.Project.PathWithNamespace, branchFromRef(p.Ref), changedFiles(p.Commits)}}}, true

	case "gitea":
		// Gitea signs the body with HMAC-SHA256, hex, no prefix. Forgejo - and
		// so Codeberg - sends the same delivery under its own header names as
		// well, which is why both are accepted.
		sig := r.Header.Get("X-Gitea-Signature")
		if sig == "" {
			sig = r.Header.Get("X-Forgejo-Signature")
		}
		if !validHexHMAC(sig, body, secret) {
			return gitPushEvent{}, false
		}
		event := r.Header.Get("X-Gitea-Event")
		if event == "" {
			event = r.Header.Get("X-Forgejo-Event")
		}
		if !strings.EqualFold(event, "push") {
			return gitPushEvent{}, true
		}
		var p struct {
			Ref        string `json:"ref"`
			Repository struct {
				FullName string `json:"full_name"`
			} `json:"repository"`
			Commits []commitFiles `json:"commits"`
		}
		if err := json.Unmarshal(body, &p); err != nil {
			return gitPushEvent{}, true
		}
		return gitPushEvent{refs: []gitPushRef{{p.Repository.FullName, branchFromRef(p.Ref), changedFiles(p.Commits)}}}, true

	case "bitbucket":
		// Bitbucket signs with HMAC-SHA256 under the WebSub spelling,
		// "sha256=<hex>", the same shape GitHub uses.
		if !validateGitHubSignature(r.Header.Get("X-Hub-Signature"), body, secret) {
			return gitPushEvent{}, false
		}
		if !strings.EqualFold(r.Header.Get("X-Event-Key"), "repo:push") {
			return gitPushEvent{}, true
		}
		var p struct {
			Repository struct {
				FullName string `json:"full_name"`
			} `json:"repository"`
			Push struct {
				Changes []struct {
					New struct {
						Type string `json:"type"`
						Name string `json:"name"`
					} `json:"new"`
				} `json:"changes"`
			} `json:"push"`
		}
		if err := json.Unmarshal(body, &p); err != nil {
			return gitPushEvent{}, true
		}
		// One delivery can carry several branches, and a deleted branch comes
		// through with an empty "new".
		var refs []gitPushRef
		for _, c := range p.Push.Changes {
			if c.New.Type != "branch" {
				continue
			}
			// Bitbucket's push payload names no files, so every push on a
			// watched branch builds whatever the watched paths say.
			refs = append(refs, gitPushRef{p.Repository.FullName, c.New.Name, nil})
		}
		return gitPushEvent{refs: refs}, true
	}
	return gitPushEvent{}, false
}

// validHexHMAC checks a bare hex HMAC-SHA256 of the body.
func validHexHMAC(sig string, body []byte, secret string) bool {
	if sig == "" || secret == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	want := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(want), []byte(sig))
}

package handler

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// GitHubPushPayload captures the minimal fields needed from a GitHub push event.
type gitHubPushPayload struct {
	Ref        string `json:"ref"` // e.g. "refs/heads/main"
	Repository struct {
		FullName string `json:"full_name"` // e.g. "owner/repo"
	} `json:"repository"`
}

// GitHubWebhook handles POST /api/v1/webhooks/github/{integrationId}.
// GitHub sends this for every push event on repos accessible to the installed App.
// We validate X-Hub-Signature-256, extract repo+branch, and trigger auto-deploy
// for every service tracking that repo/branch.
func (h *Handler) GitHubWebhook(w http.ResponseWriter, r *http.Request) {
	integrationIDStr := chi.URLParam(r, "integrationId")
	integrationID, err := uuid.Parse(integrationIDStr)
	if err != nil {
		http.Error(w, "invalid integration ID", http.StatusBadRequest)
		return
	}

	// Load the integration to get the webhook secret.
	integration, err := h.svc.GitIntegrations.GetByID(r.Context(), integrationID)
	if err != nil {
		// Return 200 so GitHub doesn't keep retrying for stale integration IDs.
		w.WriteHeader(http.StatusOK)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 10<<20)) // 10 MiB limit
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	// Validate HMAC-SHA256 signature.
	if !validateGitHubSignature(r.Header.Get("X-Hub-Signature-256"), body, string(integration.GHWebhookSecret)) {
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}

	// Only handle push events.
	if r.Header.Get("X-GitHub-Event") != "push" {
		w.WriteHeader(http.StatusOK)
		return
	}

	var payload gitHubPushPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		http.Error(w, "invalid JSON payload", http.StatusBadRequest)
		return
	}

	// Extract branch name from ref (e.g. "refs/heads/main" → "main").
	branch := branchFromRef(payload.Ref)
	if branch == "" {
		// Tag push or other non-branch ref — ignore.
		w.WriteHeader(http.StatusOK)
		return
	}

	h.svc.Deployments.FindAndTriggerForPush(r.Context(), integrationID, payload.Repository.FullName, branch)
	w.WriteHeader(http.StatusOK)
}

// DeployWebhook handles POST /api/v1/webhooks/deploy/{serviceId}?token=xxx.
// Intended for public repos or any git provider without a native app integration.
// The user copies this URL (with their service-specific token) into their repo's
// webhook settings — any POST triggers a new build.
func (h *Handler) DeployWebhook(w http.ResponseWriter, r *http.Request) {
	serviceIDStr := chi.URLParam(r, "serviceId")
	serviceID, err := uuid.Parse(serviceIDStr)
	if err != nil {
		http.Error(w, "invalid service ID", http.StatusBadRequest)
		return
	}

	token := r.URL.Query().Get("token")
	_, err = h.svc.Deployments.TriggerByDeployToken(r.Context(), serviceID, token)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	w.WriteHeader(http.StatusAccepted)
	fmt.Fprintln(w, `{"ok":true}`)
}

// validateGitHubSignature compares "sha256=<hex>" against HMAC-SHA256(secret, body).
func validateGitHubSignature(sigHeader string, body []byte, secret string) bool {
	if secret == "" {
		return false
	}
	const prefix = "sha256="
	if len(sigHeader) <= len(prefix) || sigHeader[:len(prefix)] != prefix {
		return false
	}
	expected := sigHeader[len(prefix):]
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	got := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(got), []byte(expected))
}

// branchFromRef strips "refs/heads/" prefix. Returns "" for tag refs or unknown formats.
func branchFromRef(ref string) string {
	const prefix = "refs/heads/"
	if len(ref) > len(prefix) && ref[:len(prefix)] == prefix {
		return ref[len(prefix):]
	}
	return ""
}

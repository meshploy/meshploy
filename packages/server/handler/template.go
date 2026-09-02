package handler

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/go-chi/chi/v5"
	"github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/templates"
)

// ── I/O types ───────────────────────────────────────────────────────────────

type ListTemplatesOutput struct {
	Body []*templates.Manifest
}

type TemplatePathInput struct {
	TemplateID string `path:"templateId"`
}

// TemplateDetail carries the manifest plus the raw compose so the web UI can
// prefill the stack editor. Variable *declarations* only — never resolved values.
type TemplateDetail struct {
	Manifest *templates.Manifest `json:"manifest"`
	Compose  string              `json:"compose"`
}

type GetTemplateOutput struct {
	Body TemplateDetail
}

type DeployTemplateBody struct {
	// Spec is the (possibly user-edited) compose from the stack editor; empty
	// means use the template's own compose.
	Spec         string            `json:"spec,omitempty"`
	PromptValues map[string]string `json:"prompt_values,omitempty"`
}

type DeployTemplateInput struct {
	OrgID      string `path:"orgId"`
	ProjectID  string `path:"projectId"`
	TemplateID string `path:"templateId"`
	Body       DeployTemplateBody
}

// ── Registration ─────────────────────────────────────────────────────────────

func (h *Handler) registerTemplateRoutes(api huma.API) {
	// Re-read the catalog now. Any authenticated user may ask: it refetches a
	// public index and changes nothing else, and the person who just published a
	// template is not necessarily an admin.
	huma.Register(api, huma.Operation{
		OperationID: "refresh-templates",
		Method:      "POST",
		Path:        "/api/v1/templates/refresh",
		Summary:     "Re-read the template catalog from its source",
		Tags:        []string{"Templates"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.RefreshTemplates)

	huma.Register(api, huma.Operation{
		OperationID: "list-templates",
		Method:      "GET",
		Path:        "/api/v1/templates",
		Summary:     "List one-click templates",
		Tags:        []string{"Templates"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.ListTemplates)

	huma.Register(api, huma.Operation{
		OperationID: "get-template",
		Method:      "GET",
		Path:        "/api/v1/templates/{templateId}",
		Summary:     "Get a template (manifest + compose)",
		Tags:        []string{"Templates"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.GetTemplate)

	huma.Register(api, huma.Operation{
		OperationID:   "deploy-template",
		Method:        "POST",
		Path:          "/api/v1/orgs/{orgId}/projects/{projectId}/templates/{templateId}/deploy",
		Summary:       "Deploy a template into a project as a stack",
		Tags:          []string{"Templates"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: 201,
	}, h.DeployTemplate)
}

// ── Handlers ─────────────────────────────────────────────────────────────────

func (h *Handler) ListTemplates(ctx context.Context, _ *struct{}) (*ListTemplatesOutput, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	list, err := h.svc.Templates.List()
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to list templates", err)
	}
	return &ListTemplatesOutput{Body: list}, nil
}

func (h *Handler) GetTemplate(ctx context.Context, input *TemplatePathInput) (*GetTemplateOutput, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	tpl, err := h.svc.Templates.Get(input.TemplateID)
	if err != nil {
		return nil, huma.Error404NotFound("template not found")
	}
	return &GetTemplateOutput{Body: TemplateDetail{Manifest: tpl.Manifest, Compose: tpl.Compose}}, nil
}

// RefreshTemplatesInput carries no parameters — the catalog is a single global
// source, not a per-org one.
type RefreshTemplatesInput struct{}

type RefreshTemplatesOutput struct {
	Body struct {
		Count int `json:"count"`
	}
}

// RefreshTemplates re-reads the catalog and reports how many templates it now
// holds, so the caller can see the refresh took effect rather than trusting it.
func (h *Handler) RefreshTemplates(ctx context.Context, _ *RefreshTemplatesInput) (*RefreshTemplatesOutput, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	if err := h.svc.Templates.Refresh(ctx); err != nil {
		return nil, huma.Error502BadGateway("could not reach the template catalog: " + err.Error())
	}
	list, err := h.svc.Templates.List()
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	out := &RefreshTemplatesOutput{}
	out.Body.Count = len(list)
	return out, nil
}

// ServeTemplateIcon streams a template's icon bytes. It is a raw, unauthenticated
// route (registered in RegisterRaw) so it works as an <img src>; template icons
// are public catalog data.
func (h *Handler) ServeTemplateIcon(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "templateId")
	data, contentType, err := h.svc.Templates.Icon(id)
	if err != nil {
		http.Error(w, "icon not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_, _ = w.Write(data)
}

func (h *Handler) DeployTemplate(ctx context.Context, input *DeployTemplateInput) (*GetStackOutput, error) {
	userID, _, projectID, _, err := h.checkAccess(ctx, input.OrgID, input.ProjectID, db.ResourceProject, db.ActionCreate, "")
	if err != nil {
		return nil, err
	}
	stack, err := h.svc.Templates.Deploy(ctx, projectID, input.TemplateID, input.Body.Spec, input.Body.PromptValues, userID)
	if err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	return &GetStackOutput{Body: stack}, nil
}

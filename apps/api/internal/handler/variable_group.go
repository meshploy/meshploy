package handler

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/meshploy/apps/api/internal/service"
	"github.com/meshploy/packages/db"
)

// ─── Response types ───────────────────────────────────────────────────────────

type ApiVariableGroupItem struct {
	ID       string `json:"id"`
	GroupID  string `json:"group_id"`
	Key      string `json:"key"`
	Value    string `json:"value,omitempty"` // omitted for is_secret items in list responses
	IsSecret bool   `json:"is_secret"`
}

type ApiVariableGroup struct {
	ID            string                 `json:"id"`
	ProjectID     string                 `json:"project_id"`
	ServiceID     *string                `json:"service_id,omitempty"`
	Name          string                 `json:"name"`
	Description   string                 `json:"description"`
	SystemManaged bool                   `json:"system_managed"`
	Items         []ApiVariableGroupItem `json:"items"`
	CreatedAt     string                 `json:"created_at"`
	UpdatedAt     string                 `json:"updated_at"`
}

type ApiAttachedGroup struct {
	ServiceID string           `json:"service_id"`
	Group     ApiVariableGroup `json:"group"`
}

func toApiGroup(g db.VariableGroup, revealSecrets bool) ApiVariableGroup {
	items := make([]ApiVariableGroupItem, len(g.Items))
	for i, it := range g.Items {
		item := ApiVariableGroupItem{
			ID:       it.ID.String(),
			GroupID:  it.GroupID.String(),
			Key:      it.Key,
			IsSecret: it.IsSecret,
		}
		if !it.IsSecret || revealSecrets {
			item.Value = string(it.Value)
		}
		items[i] = item
	}
	var svcID *string
	if g.ServiceID != nil {
		s := g.ServiceID.String()
		svcID = &s
	}
	return ApiVariableGroup{
		ID:            g.ID.String(),
		ProjectID:     g.ProjectID.String(),
		ServiceID:     svcID,
		Name:          g.Name,
		Description:   g.Description,
		SystemManaged: g.SystemManaged,
		Items:         items,
		CreatedAt:     g.CreatedAt.String(),
		UpdatedAt:     g.UpdatedAt.String(),
	}
}

// ─── List groups ──────────────────────────────────────────────────────────────

type ListVariableGroupsInput struct {
	OrgID     string `path:"orgId"`
	ProjectID string `path:"projectId"`
}
type ListVariableGroupsOutput struct {
	Body []ApiVariableGroup
}

func (h *Handler) ListVariableGroups(ctx context.Context, input *ListVariableGroupsInput) (*ListVariableGroupsOutput, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	projectID, err := parseUUID(input.ProjectID)
	if err != nil {
		return nil, err
	}
	groups, err := h.svc.VariableGroups.List(ctx, projectID)
	if err != nil {
		return nil, err
	}
	out := make([]ApiVariableGroup, len(groups))
	for i, g := range groups {
		out[i] = toApiGroup(g, false)
	}
	return &ListVariableGroupsOutput{Body: out}, nil
}

// ─── Create group ─────────────────────────────────────────────────────────────

type CreateVariableGroupInput struct {
	OrgID     string `path:"orgId"`
	ProjectID string `path:"projectId"`
	Body      struct {
		Name        string `json:"name"`
		Description string `json:"description,omitempty"`
	}
}
type CreateVariableGroupOutput struct {
	Body ApiVariableGroup
}

func (h *Handler) CreateVariableGroup(ctx context.Context, input *CreateVariableGroupInput) (*CreateVariableGroupOutput, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	projectID, err := parseUUID(input.ProjectID)
	if err != nil {
		return nil, err
	}
	g, err := h.svc.VariableGroups.Create(ctx, service.CreateGroupInput{
		ProjectID:   projectID,
		Name:        input.Body.Name,
		Description: input.Body.Description,
	})
	if err != nil {
		return nil, err
	}
	g.Items = nil
	return &CreateVariableGroupOutput{Body: toApiGroup(*g, false)}, nil
}

// ─── Get group ────────────────────────────────────────────────────────────────

type GetVariableGroupInput struct {
	OrgID     string `path:"orgId"`
	ProjectID string `path:"projectId"`
	GroupID   string `path:"groupId"`
}
type GetVariableGroupOutput struct {
	Body ApiVariableGroup
}

func (h *Handler) GetVariableGroup(ctx context.Context, input *GetVariableGroupInput) (*GetVariableGroupOutput, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	groupID, err := parseUUID(input.GroupID)
	if err != nil {
		return nil, err
	}
	g, err := h.svc.VariableGroups.Get(ctx, groupID)
	if err != nil {
		return nil, err
	}
	return &GetVariableGroupOutput{Body: toApiGroup(*g, false)}, nil
}

// ─── Update group ─────────────────────────────────────────────────────────────

type UpdateVariableGroupInput struct {
	OrgID     string `path:"orgId"`
	ProjectID string `path:"projectId"`
	GroupID   string `path:"groupId"`
	Body      struct {
		Name        *string `json:"name,omitempty"`
		Description *string `json:"description,omitempty"`
	}
}

func (h *Handler) UpdateVariableGroup(ctx context.Context, input *UpdateVariableGroupInput) (*GetVariableGroupOutput, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	groupID, err := parseUUID(input.GroupID)
	if err != nil {
		return nil, err
	}
	g, err := h.svc.VariableGroups.Update(ctx, groupID, service.UpdateGroupInput{
		Name:        input.Body.Name,
		Description: input.Body.Description,
	})
	if err != nil {
		return nil, err
	}
	return &GetVariableGroupOutput{Body: toApiGroup(*g, false)}, nil
}

// ─── Delete group ─────────────────────────────────────────────────────────────

type DeleteVariableGroupInput struct {
	OrgID     string `path:"orgId"`
	ProjectID string `path:"projectId"`
	GroupID   string `path:"groupId"`
}

func (h *Handler) DeleteVariableGroup(ctx context.Context, input *DeleteVariableGroupInput) (*struct{}, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	groupID, err := parseUUID(input.GroupID)
	if err != nil {
		return nil, err
	}
	return nil, h.svc.VariableGroups.Delete(ctx, groupID)
}

// ─── Upsert item ──────────────────────────────────────────────────────────────

type UpsertVariableGroupItemInput struct {
	OrgID     string `path:"orgId"`
	ProjectID string `path:"projectId"`
	GroupID   string `path:"groupId"`
	Body      struct {
		Key      string `json:"key"`
		Value    string `json:"value"`
		IsSecret bool   `json:"is_secret"`
	}
}
type UpsertVariableGroupItemOutput struct {
	Body ApiVariableGroupItem
}

func (h *Handler) UpsertVariableGroupItem(ctx context.Context, input *UpsertVariableGroupItemInput) (*UpsertVariableGroupItemOutput, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	groupID, err := parseUUID(input.GroupID)
	if err != nil {
		return nil, err
	}
	item, err := h.svc.VariableGroups.UpsertItem(ctx, groupID, service.UpsertItemInput{
		Key:      input.Body.Key,
		Value:    input.Body.Value,
		IsSecret: input.Body.IsSecret,
	})
	if err != nil {
		return nil, err
	}
	out := ApiVariableGroupItem{
		ID:       item.ID.String(),
		GroupID:  item.GroupID.String(),
		Key:      item.Key,
		Value:    string(item.Value), // caller just set it, so reveal it
		IsSecret: item.IsSecret,
	}
	return &UpsertVariableGroupItemOutput{Body: out}, nil
}

// ─── Delete item ──────────────────────────────────────────────────────────────

type DeleteVariableGroupItemInput struct {
	OrgID     string `path:"orgId"`
	ProjectID string `path:"projectId"`
	GroupID   string `path:"groupId"`
	ItemID    string `path:"itemId"`
}

func (h *Handler) DeleteVariableGroupItem(ctx context.Context, input *DeleteVariableGroupItemInput) (*struct{}, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	itemID, err := parseUUID(input.ItemID)
	if err != nil {
		return nil, err
	}
	return nil, h.svc.VariableGroups.DeleteItem(ctx, itemID)
}

// ─── List groups for a service ────────────────────────────────────────────────

type ListServiceVariableGroupsInput struct {
	OrgID     string `path:"orgId"`
	ProjectID string `path:"projectId"`
	ServiceID string `path:"serviceId"`
}
type ListServiceVariableGroupsOutput struct {
	Body []ApiVariableGroup
}

func (h *Handler) ListServiceVariableGroups(ctx context.Context, input *ListServiceVariableGroupsInput) (*ListServiceVariableGroupsOutput, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	serviceID, err := parseUUID(input.ServiceID)
	if err != nil {
		return nil, err
	}
	groups, err := h.svc.VariableGroups.ListForService(ctx, serviceID)
	if err != nil {
		return nil, err
	}
	out := make([]ApiVariableGroup, len(groups))
	for i, g := range groups {
		out[i] = toApiGroup(g, false)
	}
	return &ListServiceVariableGroupsOutput{Body: out}, nil
}

// ─── Attach / detach ─────────────────────────────────────────────────────────

type AttachVariableGroupInput struct {
	OrgID     string `path:"orgId"`
	ProjectID string `path:"projectId"`
	ServiceID string `path:"serviceId"`
	Body      struct {
		GroupID string `json:"group_id"`
	}
}

func (h *Handler) AttachVariableGroup(ctx context.Context, input *AttachVariableGroupInput) (*struct{}, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	serviceID, err := parseUUID(input.ServiceID)
	if err != nil {
		return nil, err
	}
	groupID, err := uuid.Parse(input.Body.GroupID)
	if err != nil {
		return nil, huma.Error400BadRequest("invalid group_id")
	}
	return nil, h.svc.VariableGroups.Attach(ctx, serviceID, groupID)
}

type DetachVariableGroupInput struct {
	OrgID     string `path:"orgId"`
	ProjectID string `path:"projectId"`
	ServiceID string `path:"serviceId"`
	GroupID   string `path:"groupId"`
}

func (h *Handler) DetachVariableGroup(ctx context.Context, input *DetachVariableGroupInput) (*struct{}, error) {
	if _, err := requireUser(ctx); err != nil {
		return nil, err
	}
	serviceID, err := parseUUID(input.ServiceID)
	if err != nil {
		return nil, err
	}
	groupID, err := parseUUID(input.GroupID)
	if err != nil {
		return nil, err
	}
	return nil, h.svc.VariableGroups.Detach(ctx, serviceID, groupID)
}

// ─── Route registration ───────────────────────────────────────────────────────

func (h *Handler) registerVariableGroupRoutes(api huma.API) {
	const tag = "Variable Groups"
	const base = "/api/v1/orgs/{orgId}/projects/{projectId}"

	huma.Register(api, huma.Operation{OperationID: "list-variable-groups", Method: "GET", Path: base + "/variable-groups", Summary: "List variable groups", Tags: []string{tag}, Security: []map[string][]string{{"bearer": {}}}}, h.ListVariableGroups)
	huma.Register(api, huma.Operation{OperationID: "create-variable-group", Method: "POST", Path: base + "/variable-groups", Summary: "Create variable group", Tags: []string{tag}, Security: []map[string][]string{{"bearer": {}}}}, h.CreateVariableGroup)
	huma.Register(api, huma.Operation{OperationID: "get-variable-group", Method: "GET", Path: base + "/variable-groups/{groupId}", Summary: "Get variable group", Tags: []string{tag}, Security: []map[string][]string{{"bearer": {}}}}, h.GetVariableGroup)
	huma.Register(api, huma.Operation{OperationID: "update-variable-group", Method: "PATCH", Path: base + "/variable-groups/{groupId}", Summary: "Update variable group", Tags: []string{tag}, Security: []map[string][]string{{"bearer": {}}}}, h.UpdateVariableGroup)
	huma.Register(api, huma.Operation{OperationID: "delete-variable-group", Method: "DELETE", Path: base + "/variable-groups/{groupId}", Summary: "Delete variable group", Tags: []string{tag}, Security: []map[string][]string{{"bearer": {}}}}, h.DeleteVariableGroup)

	huma.Register(api, huma.Operation{OperationID: "upsert-variable-group-item", Method: "PUT", Path: base + "/variable-groups/{groupId}/items", Summary: "Upsert variable group item", Tags: []string{tag}, Security: []map[string][]string{{"bearer": {}}}}, h.UpsertVariableGroupItem)
	huma.Register(api, huma.Operation{OperationID: "delete-variable-group-item", Method: "DELETE", Path: base + "/variable-groups/{groupId}/items/{itemId}", Summary: "Delete variable group item", Tags: []string{tag}, Security: []map[string][]string{{"bearer": {}}}}, h.DeleteVariableGroupItem)

	huma.Register(api, huma.Operation{OperationID: "list-service-variable-groups", Method: "GET", Path: base + "/services/{serviceId}/variable-groups", Summary: "List variable groups attached to service", Tags: []string{tag}, Security: []map[string][]string{{"bearer": {}}}}, h.ListServiceVariableGroups)
	huma.Register(api, huma.Operation{OperationID: "attach-variable-group", Method: "POST", Path: base + "/services/{serviceId}/variable-groups", Summary: "Attach variable group to service", Tags: []string{tag}, Security: []map[string][]string{{"bearer": {}}}}, h.AttachVariableGroup)
	huma.Register(api, huma.Operation{OperationID: "detach-variable-group", Method: "DELETE", Path: base + "/services/{serviceId}/variable-groups/{groupId}", Summary: "Detach variable group from service", Tags: []string{tag}, Security: []map[string][]string{{"bearer": {}}}}, h.DetachVariableGroup)
}

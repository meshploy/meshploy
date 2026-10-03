package handler

import (
	"context"
	"errors"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/meshploy/packages/server/service"
	"gorm.io/gorm"
)

type AccessRulesInput struct {
	OrgID string `path:"orgId"`
}

type AccessRulesOutput struct {
	Body []service.AccessRule
}

type AddAccessRuleInput struct {
	OrgID string `path:"orgId"`
	Body  struct {
		FromKind string `json:"from_kind" enum:"person,machine,all"`
		FromID   string `json:"from_id,omitempty"`
		ToNodeID string `json:"to_node_id"`
		Ports    string `json:"ports" doc:"Comma-separated; empty for every port"`
		Note     string `json:"note" maxLength:"500"`
	}
}

type AccessRuleIDInput struct {
	OrgID  string `path:"orgId"`
	RuleID string `path:"ruleId"`
}

type PreviewAccessRuleOutput struct {
	Body struct {
		ACLs string `json:"acls"`
	}
}

func (h *Handler) registerAccessRuleRoutes(api huma.API) {
	sec := []map[string][]string{{"bearer": {}}}
	huma.Register(api, huma.Operation{OperationID: "list-access-rules", Method: "GET", Path: "/api/v1/orgs/{orgId}/access/rules",
		Summary: "Every grant and network rule: who reaches what, on which ports", Tags: []string{"Access"}, Security: sec}, h.ListAccessRules)
	huma.Register(api, huma.Operation{OperationID: "add-access-rule", Method: "POST", Path: "/api/v1/orgs/{orgId}/access/rules",
		Summary: "Add a network rule: a machine, and ports on it, for a person, a machine or every machine", Tags: []string{"Access"},
		Security: sec, DefaultStatus: 201}, h.AddAccessRule)
	huma.Register(api, huma.Operation{OperationID: "preview-access-rule", Method: "POST", Path: "/api/v1/orgs/{orgId}/access/rules/preview",
		Summary: "What a network rule would add to the mesh's policy", Tags: []string{"Access"}, Security: sec}, h.PreviewAccessRule)
	huma.Register(api, huma.Operation{OperationID: "update-access-rule", Method: "PUT", Path: "/api/v1/orgs/{orgId}/access/rules/{ruleId}",
		Summary: "Change a network rule", Tags: []string{"Access"}, Security: sec, DefaultStatus: 204}, h.UpdateAccessRule)
	huma.Register(api, huma.Operation{OperationID: "route-openers", Method: "GET", Path: "/api/v1/orgs/{orgId}/projects/{projectId}/routes/{routeId}/openers",
		Summary: "Who an internal route answers on the mesh, once the mesh policy is enforced", Tags: []string{"Access"}, Security: sec}, h.RouteOpeners)
	huma.Register(api, huma.Operation{OperationID: "remove-access-rule", Method: "DELETE", Path: "/api/v1/orgs/{orgId}/access/rules/{ruleId}",
		Summary: "Remove a network rule", Tags: []string{"Access"}, Security: sec, DefaultStatus: 204}, h.RemoveAccessRule)
}

func (h *Handler) ListAccessRules(ctx context.Context, input *AccessRulesInput) (*AccessRulesOutput, error) {
	_, orgID, _, err := h.checkOrgAdminAccess(ctx, input.OrgID, "")
	if err != nil {
		return nil, err
	}
	rules, err := h.svc.MeshAccess.Rules(ctx, orgID)
	if err != nil {
		return nil, err
	}
	return &AccessRulesOutput{Body: rules}, nil
}

func ruleInput(in *AddAccessRuleInput) (service.AccessRuleInput, error) {
	to, err := uuid.Parse(in.Body.ToNodeID)
	if err != nil {
		return service.AccessRuleInput{}, huma.Error400BadRequest("choose a machine as the destination")
	}
	out := service.AccessRuleInput{FromKind: in.Body.FromKind, ToNodeID: to, Ports: in.Body.Ports, Note: in.Body.Note}
	if in.Body.FromID != "" {
		id, err := uuid.Parse(in.Body.FromID)
		if err != nil {
			return out, huma.Error400BadRequest("invalid source")
		}
		out.FromID = &id
	}
	return out, nil
}

func (h *Handler) AddAccessRule(ctx context.Context, input *AddAccessRuleInput) (*struct{}, error) {
	userID, orgID, _, err := h.checkOrgAdminAccess(ctx, input.OrgID, "")
	if err != nil {
		return nil, err
	}
	in, err := ruleInput(input)
	if err != nil {
		return nil, err
	}
	if _, err := h.svc.MeshAccess.AddRule(ctx, orgID, userID, in); errors.Is(err, service.ErrAccessRule) || errors.Is(err, service.ErrRuleFromCluster) {
		return nil, huma.Error400BadRequest(err.Error())
	} else if err != nil {
		return nil, err
	}
	return nil, nil
}

type UpdateAccessRuleInput struct {
	RuleID string `path:"ruleId"`
	AddAccessRuleInput
}

func (h *Handler) UpdateAccessRule(ctx context.Context, input *UpdateAccessRuleInput) (*struct{}, error) {
	_, orgID, _, err := h.checkOrgAdminAccess(ctx, input.OrgID, "")
	if err != nil {
		return nil, err
	}
	id, err := uuid.Parse(input.RuleID)
	if err != nil {
		return nil, huma.Error400BadRequest("invalid rule id")
	}
	in, err := ruleInput(&input.AddAccessRuleInput)
	if err != nil {
		return nil, err
	}
	switch err := h.svc.MeshAccess.UpdateRule(ctx, orgID, id, in); {
	case errors.Is(err, service.ErrAccessRule), errors.Is(err, service.ErrRuleFromCluster):
		return nil, huma.Error400BadRequest(err.Error())
	case err != nil:
		return nil, err
	}
	return nil, nil
}

func (h *Handler) PreviewAccessRule(ctx context.Context, input *AddAccessRuleInput) (*PreviewAccessRuleOutput, error) {
	_, orgID, _, err := h.checkOrgAdminAccess(ctx, input.OrgID, "")
	if err != nil {
		return nil, err
	}
	in, err := ruleInput(input)
	if err != nil {
		return nil, err
	}
	acls, err := h.svc.MeshAccess.PreviewRule(ctx, orgID, in)
	if err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	out := &PreviewAccessRuleOutput{}
	out.Body.ACLs = acls
	return out, nil
}

func (h *Handler) RemoveAccessRule(ctx context.Context, input *AccessRuleIDInput) (*struct{}, error) {
	_, orgID, _, err := h.checkOrgAdminAccess(ctx, input.OrgID, "")
	if err != nil {
		return nil, err
	}
	id, err := uuid.Parse(input.RuleID)
	if err != nil {
		return nil, huma.Error400BadRequest("invalid rule id")
	}
	if err := h.svc.MeshAccess.RemoveRule(ctx, orgID, id); errors.Is(err, service.ErrAccessRule) {
		return nil, huma.Error404NotFound("rule not found")
	} else if err != nil {
		return nil, err
	}
	return nil, nil
}

type RouteOpenersInput struct {
	OrgID     string `path:"orgId"`
	ProjectID string `path:"projectId"`
	RouteID   string `path:"routeId"`
}

type RouteOpenersOutput struct {
	Body service.RouteOpeners
}

func (h *Handler) RouteOpeners(ctx context.Context, input *RouteOpenersInput) (*RouteOpenersOutput, error) {
	_, orgID, _, err := h.checkOrgAdminAccess(ctx, input.OrgID, "")
	if err != nil {
		return nil, err
	}
	routeID, err := uuid.Parse(input.RouteID)
	if err != nil {
		return nil, huma.Error400BadRequest("invalid route id")
	}
	out, err := h.svc.MeshAccess.RouteOpeners(ctx, orgID, routeID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, huma.Error404NotFound("route not found")
	}
	if err != nil {
		return nil, err
	}
	return &RouteOpenersOutput{Body: out}, nil
}

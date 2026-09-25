package handler

import (
	"context"
	"errors"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
	svc "github.com/meshploy/packages/server/service"
)

// Promotion groups, promotion, and the board. See service/promotion.go.

type ListGroupsOutput struct {
	Body []svc.GroupView
}

type CreateGroupInput struct {
	OrgID     string `path:"orgId"`
	ProjectID string `path:"projectId"`
	Body      struct {
		Name       string   `json:"name" minLength:"1" maxLength:"60"`
		ServiceIDs []string `json:"service_ids" minItems:"1"`
		// Path lists level project IDs from the level the group enters at, the
		// only one that builds, up to production.
		Path []string `json:"path" minItems:"2"`
	}
}

type CreateGroupOutput struct {
	Body *db.PromotionGroup
}

type GroupPathInput struct {
	OrgID     string `path:"orgId"`
	ProjectID string `path:"projectId"`
	GroupID   string `path:"groupId"`
}

type PromoteOutput struct {
	Body *svc.PromoteResult
}

type BoardOutput struct {
	Body *svc.Board
}

func (h *Handler) registerPromotionRoutes(api huma.API) {
	base := "/api/v1/orgs/{orgId}/projects/{projectId}"
	huma.Register(api, huma.Operation{
		OperationID: "get-board", Method: "GET", Path: base + "/board",
		Summary: "A project's levels, groups, and what each service runs at each level",
		Tags:    []string{"Projects"}, Security: []map[string][]string{{"bearer": {}}},
	}, h.GetBoard)
	huma.Register(api, huma.Operation{
		OperationID: "list-promotion-groups", Method: "GET", Path: base + "/promotion-groups",
		Summary: "List a project's promotion groups",
		Tags:    []string{"Projects"}, Security: []map[string][]string{{"bearer": {}}},
	}, h.ListPromotionGroups)
	huma.Register(api, huma.Operation{
		OperationID: "create-promotion-group", Method: "POST", Path: base + "/promotion-groups",
		Summary: "Create a promotion group, putting its services into the level it enters at",
		Tags:    []string{"Projects"}, Security: []map[string][]string{{"bearer": {}}}, DefaultStatus: 201,
	}, h.CreatePromotionGroup)
	huma.Register(api, huma.Operation{
		OperationID: "delete-promotion-group", Method: "DELETE", Path: base + "/promotion-groups/{groupId}",
		Summary: "Delete a promotion group; its services stay where they are",
		Tags:    []string{"Projects"}, Security: []map[string][]string{{"bearer": {}}}, DefaultStatus: 204,
	}, h.DeletePromotionGroup)
	huma.Register(api, huma.Operation{
		OperationID: "rename-promotion-group", Method: "PATCH", Path: base + "/promotion-groups/{groupId}",
		Summary: "Rename a promotion group",
		Tags:    []string{"Projects"}, Security: []map[string][]string{{"bearer": {}}}, DefaultStatus: 204,
	}, h.RenamePromotionGroup)
	huma.Register(api, huma.Operation{
		OperationID: "add-to-promotion-group", Method: "POST", Path: base + "/promotion-groups/{groupId}/services",
		Summary: "Add services to a promotion group, merging away any single-service group they were in",
		Tags:    []string{"Projects"}, Security: []map[string][]string{{"bearer": {}}}, DefaultStatus: 204,
	}, h.AddToPromotionGroup)
	huma.Register(api, huma.Operation{
		OperationID: "remove-from-promotion-group", Method: "DELETE", Path: base + "/promotion-groups/{groupId}/services/{lineageId}",
		Summary: "Take a service out of a promotion group; its copies stay where they are",
		Tags:    []string{"Projects"}, Security: []map[string][]string{{"bearer": {}}},
	}, h.RemoveFromPromotionGroup)
	huma.Register(api, huma.Operation{
		OperationID: "copy-service-to-level", Method: "POST", Path: base + "/services/{serviceId}/copy-to-level",
		Summary: "Copy a service into a lower level as a group of its own",
		Tags:    []string{"Projects"}, Security: []map[string][]string{{"bearer": {}}}, DefaultStatus: 201,
	}, h.CopyServiceToLevel)
	huma.Register(api, huma.Operation{
		OperationID: "bring-service-down", Method: "POST", Path: base + "/services/{serviceId}/bring-down",
		Summary: "Run a service's current image in a lower level, to reproduce what runs above",
		Tags:    []string{"Projects"}, Security: []map[string][]string{{"bearer": {}}},
	}, h.BringServiceDown)
	huma.Register(api, huma.Operation{
		OperationID: "remove-service-from-level", Method: "DELETE", Path: base + "/services/{serviceId}/level-copy",
		Summary: "Delete this level's copy of a service; production's is untouched and the level uses the one above",
		Tags:    []string{"Projects"}, Security: []map[string][]string{{"bearer": {}}},
	}, h.RemoveServiceFromLevel)
	huma.Register(api, huma.Operation{
		OperationID: "own-database", Method: "POST", Path: base + "/own-database",
		Summary: "Give this level its own copy of a database it uses from above, empty or cloned",
		Tags:    []string{"Projects"}, Security: []map[string][]string{{"bearer": {}}}, DefaultStatus: 201,
	}, h.OwnDatabase)
	huma.Register(api, huma.Operation{
		OperationID: "promote-group", Method: "POST", Path: base + "/promotion-groups/{groupId}/promote",
		Summary: "Promote a group's services from this level to the next on its path, as the same images",
		Tags:    []string{"Projects"}, Security: []map[string][]string{{"bearer": {}}},
	}, h.PromoteGroup)
	huma.Register(api, huma.Operation{
		OperationID: "promote-group-preflight", Method: "GET", Path: base + "/promotion-groups/{groupId}/preflight",
		Summary: "What promoting a group from this level would create, bring along, or be stopped by",
		Tags:    []string{"Projects"}, Security: []map[string][]string{{"bearer": {}}},
	}, h.PromotePreflight)
}

type PreflightOutput struct {
	Body []svc.PreflightNote
}

func (h *Handler) PromotePreflight(ctx context.Context, input *GroupPathInput) (*PreflightOutput, error) {
	_, _, level, _, err := h.checkAccess(ctx, input.OrgID, input.ProjectID, db.ResourceProject, db.ActionView, "")
	if err != nil {
		return nil, err
	}
	groupID, err := parseUUID(input.GroupID)
	if err != nil {
		return nil, err
	}
	notes, err := h.svc.Promotions.Preflight(ctx, level, groupID)
	if err != nil {
		return nil, promotionError(err)
	}
	return &PreflightOutput{Body: notes}, nil
}

func (h *Handler) GetBoard(ctx context.Context, input *ProjectPathInput) (*BoardOutput, error) {
	_, _, projectID, _, err := h.checkAccess(ctx, input.OrgID, input.ProjectID, db.ResourceProject, db.ActionView, "")
	if err != nil {
		return nil, err
	}
	board, err := h.svc.Promotions.Board(ctx, projectID)
	if err != nil {
		return nil, notFound(err)
	}
	return &BoardOutput{Body: board}, nil
}

func (h *Handler) ListPromotionGroups(ctx context.Context, input *ProjectPathInput) (*ListGroupsOutput, error) {
	_, _, projectID, _, err := h.checkAccess(ctx, input.OrgID, input.ProjectID, db.ResourceProject, db.ActionView, "")
	if err != nil {
		return nil, err
	}
	groups, err := h.svc.Promotions.ListGroups(ctx, projectID)
	if err != nil {
		return nil, notFound(err)
	}
	return &ListGroupsOutput{Body: groups}, nil
}

func parseUUIDs(in []string) ([]uuid.UUID, error) {
	out := make([]uuid.UUID, len(in))
	for i, s := range in {
		id, err := parseUUID(s)
		if err != nil {
			return nil, err
		}
		out[i] = id
	}
	return out, nil
}

func (h *Handler) CreatePromotionGroup(ctx context.Context, input *CreateGroupInput) (*CreateGroupOutput, error) {
	_, _, projectID, _, err := h.checkAccess(ctx, input.OrgID, input.ProjectID, db.ResourceProject, db.ActionUpdate, "")
	if err != nil {
		return nil, err
	}
	services, err := parseUUIDs(input.Body.ServiceIDs)
	if err != nil {
		return nil, err
	}
	path, err := parseUUIDs(input.Body.Path)
	if err != nil {
		return nil, err
	}
	group, err := h.svc.Promotions.CreateGroup(ctx, projectID, svc.GroupInput{Name: input.Body.Name, ServiceIDs: services, Path: path})
	if err != nil {
		return nil, promotionError(err)
	}
	return &CreateGroupOutput{Body: group}, nil
}

func (h *Handler) DeletePromotionGroup(ctx context.Context, input *GroupPathInput) (*struct{}, error) {
	_, _, projectID, _, err := h.checkAccess(ctx, input.OrgID, input.ProjectID, db.ResourceProject, db.ActionUpdate, "")
	if err != nil {
		return nil, err
	}
	groupID, err := parseUUID(input.GroupID)
	if err != nil {
		return nil, err
	}
	return nil, notFound(h.svc.Promotions.DeleteGroup(ctx, projectID, groupID))
}

type PromoteGroupInput struct {
	GroupPathInput
	// Overwrite moves images older than what the level above runs, replacing
	// something built there, such as a hotfix.
	Overwrite bool `query:"overwrite"`
}

func (h *Handler) PromoteGroup(ctx context.Context, input *PromoteGroupInput) (*PromoteOutput, error) {
	userID, orgID, level, _, err := h.checkAccess(ctx, input.OrgID, input.ProjectID, db.ResourceProject, db.ActionUpdate, "")
	if err != nil {
		return nil, err
	}
	groupID, err := parseUUID(input.GroupID)
	if err != nil {
		return nil, err
	}
	// Promoting from here deploys to the level above: the caller must be able
	// to change that level too, or a grant on staging would reach production.
	next, err := h.svc.Promotions.NextLevel(ctx, level, groupID)
	if err != nil {
		return nil, promotionError(err)
	}
	if err := h.svc.Permissions.CheckAccess(ctx, orgID, userID, next, db.ResourceProject, db.ActionUpdate, &next); err != nil {
		return nil, huma.Error403Forbidden("you cannot change the level this group promotes to")
	}
	promote := h.svc.Promotions.Promote
	if input.Overwrite {
		promote = h.svc.Promotions.Overwrite
	}
	out, err := promote(ctx, level, groupID)
	if err != nil {
		return nil, promotionError(err)
	}
	return &PromoteOutput{Body: out}, nil
}

type OwnDatabaseInput struct {
	OrgID     string `path:"orgId"`
	ProjectID string `path:"projectId"`
	Body      struct {
		// SourceServiceID is the database the level uses today, in a level above.
		SourceServiceID string `json:"source_service_id" format:"uuid"`
		Mode            string `json:"mode" enum:"empty,clone" doc:"empty: a fresh database. clone: restore the source's latest backup into it once it is up"`
	}
}

type OwnDatabaseOutput struct {
	Body *db.Service
}

// OwnDatabase gives this level (projectId) its own copy of a database it uses
// from above, empty or cloned.
func (h *Handler) OwnDatabase(ctx context.Context, input *OwnDatabaseInput) (*OwnDatabaseOutput, error) {
	_, _, level, _, err := h.checkAccess(ctx, input.OrgID, input.ProjectID, db.ResourceProject, db.ActionUpdate, "")
	if err != nil {
		return nil, err
	}
	source, err := parseUUID(input.Body.SourceServiceID)
	if err != nil {
		return nil, err
	}
	own, err := h.svc.Promotions.OwnDatabase(ctx, level, source, input.Body.Mode == "clone")
	switch {
	case errors.Is(err, svc.ErrHasOwnDatabase):
		return nil, huma.Error409Conflict(err.Error())
	case errors.Is(err, svc.ErrNoBackup):
		return nil, huma.Error422UnprocessableEntity(err.Error())
	case err != nil && own == nil:
		return nil, promotionError(err)
	}
	return &OwnDatabaseOutput{Body: own}, nil
}

type RenameGroupInput struct {
	OrgID     string `path:"orgId"`
	ProjectID string `path:"projectId"`
	GroupID   string `path:"groupId"`
	Body      struct {
		Name string `json:"name" minLength:"1" maxLength:"60"`
	}
}

func (h *Handler) RenamePromotionGroup(ctx context.Context, input *RenameGroupInput) (*struct{}, error) {
	_, _, projectID, _, err := h.checkAccess(ctx, input.OrgID, input.ProjectID, db.ResourceProject, db.ActionUpdate, "")
	if err != nil {
		return nil, err
	}
	groupID, err := parseUUID(input.GroupID)
	if err != nil {
		return nil, err
	}
	return nil, promotionErrorOrNil(h.svc.Promotions.RenameGroup(ctx, projectID, groupID, input.Body.Name))
}

type AddToGroupInput struct {
	OrgID     string `path:"orgId"`
	ProjectID string `path:"projectId"`
	GroupID   string `path:"groupId"`
	Body      struct {
		ServiceIDs []string `json:"service_ids" minItems:"1"`
	}
}

func (h *Handler) AddToPromotionGroup(ctx context.Context, input *AddToGroupInput) (*struct{}, error) {
	_, _, projectID, _, err := h.checkAccess(ctx, input.OrgID, input.ProjectID, db.ResourceProject, db.ActionUpdate, "")
	if err != nil {
		return nil, err
	}
	groupID, err := parseUUID(input.GroupID)
	if err != nil {
		return nil, err
	}
	ids, err := parseUUIDs(input.Body.ServiceIDs)
	if err != nil {
		return nil, err
	}
	return nil, promotionErrorOrNil(h.svc.Promotions.AddToGroup(ctx, projectID, groupID, ids))
}

type RemoveFromGroupInput struct {
	OrgID     string `path:"orgId"`
	ProjectID string `path:"projectId"`
	GroupID   string `path:"groupId"`
	LineageID string `path:"lineageId"`
}

type RemoveFromGroupOutput struct {
	Body struct {
		// GroupDeleted is set when the service was the group's last.
		GroupDeleted bool `json:"group_deleted"`
	}
}

func (h *Handler) RemoveFromPromotionGroup(ctx context.Context, input *RemoveFromGroupInput) (*RemoveFromGroupOutput, error) {
	_, _, projectID, _, err := h.checkAccess(ctx, input.OrgID, input.ProjectID, db.ResourceProject, db.ActionUpdate, "")
	if err != nil {
		return nil, err
	}
	groupID, err := parseUUID(input.GroupID)
	if err != nil {
		return nil, err
	}
	lineage, err := parseUUID(input.LineageID)
	if err != nil {
		return nil, err
	}
	deleted, err := h.svc.Promotions.RemoveFromGroup(ctx, projectID, groupID, lineage)
	if err != nil {
		return nil, promotionError(err)
	}
	out := &RemoveFromGroupOutput{}
	out.Body.GroupDeleted = deleted
	return out, nil
}

type ServiceToLevelInput struct {
	OrgID     string `path:"orgId"`
	ProjectID string `path:"projectId"`
	ServiceID string `path:"serviceId"`
	Body      struct {
		LevelID string `json:"level_id" format:"uuid" doc:"A level below the service's own"`
	}
}

// targetLevel checks the caller may change the level a copy or bring-down
// writes to, which is not the level in the URL.
func (h *Handler) targetLevel(ctx context.Context, orgID, userID uuid.UUID, levelStr string) (uuid.UUID, error) {
	level, err := parseUUID(levelStr)
	if err != nil {
		return uuid.Nil, err
	}
	if err := h.svc.Permissions.CheckAccess(ctx, orgID, userID, level, db.ResourceProject, db.ActionUpdate, &level); err != nil {
		return uuid.Nil, huma.Error403Forbidden("you cannot change that level")
	}
	return level, nil
}

func (h *Handler) CopyServiceToLevel(ctx context.Context, input *ServiceToLevelInput) (*CreateGroupOutput, error) {
	userID, orgID, _, _, err := h.checkAccess(ctx, input.OrgID, input.ServiceID, db.ResourceService, db.ActionView, input.ProjectID)
	if err != nil {
		return nil, err
	}
	level, err := h.targetLevel(ctx, orgID, userID, input.Body.LevelID)
	if err != nil {
		return nil, err
	}
	serviceID, _ := parseUUID(input.ServiceID)
	group, err := h.svc.Promotions.CopyToLevel(ctx, serviceID, level)
	if err != nil {
		return nil, promotionError(err)
	}
	return &CreateGroupOutput{Body: group}, nil
}

type BringDownOutput struct {
	Body *db.Deployment
}

func (h *Handler) BringServiceDown(ctx context.Context, input *ServiceToLevelInput) (*BringDownOutput, error) {
	userID, orgID, _, _, err := h.checkAccess(ctx, input.OrgID, input.ServiceID, db.ResourceService, db.ActionView, input.ProjectID)
	if err != nil {
		return nil, err
	}
	level, err := h.targetLevel(ctx, orgID, userID, input.Body.LevelID)
	if err != nil {
		return nil, err
	}
	serviceID, _ := parseUUID(input.ServiceID)
	dep, err := h.svc.Promotions.BringDown(ctx, serviceID, level)
	if err != nil {
		return nil, promotionError(err)
	}
	return &BringDownOutput{Body: dep}, nil
}

type RemoveFromLevelInput struct {
	OrgID     string `path:"orgId"`
	ProjectID string `path:"projectId"`
	ServiceID string `path:"serviceId"`
}

type RemoveFromLevelOutput struct {
	Body *svc.RemovedFromLevel
}

// RemoveServiceFromLevel deletes this level's copy of a service.
func (h *Handler) RemoveServiceFromLevel(ctx context.Context, input *RemoveFromLevelInput) (*RemoveFromLevelOutput, error) {
	_, _, _, _, err := h.checkAccess(ctx, input.OrgID, input.ServiceID, db.ResourceService, db.ActionDelete, input.ProjectID)
	if err != nil {
		return nil, err
	}
	serviceID, _ := parseUUID(input.ServiceID)
	projectID, _ := parseUUID(input.ProjectID)
	// The service must be in the level the URL names.
	sv, err := h.svc.Workloads.Get(ctx, serviceID, projectID)
	if err != nil || sv == nil {
		return nil, huma.Error404NotFound("service not found in this level")
	}
	out, err := h.svc.Promotions.RemoveFromLevel(ctx, serviceID)
	if err != nil {
		if errors.Is(err, svc.ErrRemoveProduction) {
			return nil, huma.Error422UnprocessableEntity(err.Error())
		}
		return nil, promotionError(err)
	}
	return &RemoveFromLevelOutput{Body: out}, nil
}

func promotionErrorOrNil(err error) error {
	if err == nil {
		return nil
	}
	return promotionError(err)
}

func promotionError(err error) error {
	switch {
	case errors.Is(err, svc.ErrGroupMember):
		return huma.Error409Conflict(err.Error())
	case errors.Is(err, svc.ErrGroupPath), errors.Is(err, svc.ErrGroupEmpty), errors.Is(err, svc.ErrGroupDatabase),
		errors.Is(err, svc.ErrGroupTopLevel), errors.Is(err, svc.ErrGroupNotHere), errors.Is(err, svc.ErrNothingToPromote),
		errors.Is(err, svc.ErrNotBelow), errors.Is(err, svc.ErrNotInGroup), errors.Is(err, svc.ErrBorrowFromBelow):
		return huma.Error422UnprocessableEntity(err.Error())
	case errors.Is(err, svc.ErrK8sNotConfigured):
		return huma.Error503ServiceUnavailable(err.Error())
	}
	return notFound(err)
}

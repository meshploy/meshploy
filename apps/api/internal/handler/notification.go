package handler

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/meshploy/apps/api/internal/service"
	meshdb "github.com/meshploy/packages/db"
)

func (h *Handler) registerNotificationRoutes(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "list-notification-channels",
		Method:      "GET",
		Path:        "/api/v1/orgs/{orgId}/notification-channels",
		Tags:        []string{"notifications"},
	}, func(ctx context.Context, in *struct {
		OrgID string `path:"orgId"`
	}) (*struct{ Body []meshdb.NotificationChannel }, error) {
		_, orgID, _, err := h.checkOrgMemberAccess(ctx, in.OrgID, "")
		if err != nil {
			return nil, err
		}
		rows, err := h.svc.Notifications.List(ctx, orgID)
		if err != nil {
			return nil, err
		}
		return &struct{ Body []meshdb.NotificationChannel }{Body: rows}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:  "create-notification-channel",
		Method:       "POST",
		Path:         "/api/v1/orgs/{orgId}/notification-channels",
		Tags:         []string{"notifications"},
		DefaultStatus: 201,
	}, func(ctx context.Context, in *struct {
		OrgID string `path:"orgId"`
		Body  struct {
			Name   string                         `json:"name"   minLength:"1"`
			Type   meshdb.NotificationChannelType `json:"type"`
			Config map[string]string              `json:"config"`
			Events []string                       `json:"events"`
		}
	}) (*struct{ Body *meshdb.NotificationChannel }, error) {
		_, orgID, _, err := h.checkOrgAdminAccess(ctx, in.OrgID, "")
		if err != nil {
			return nil, err
		}
		row, err := h.svc.Notifications.Create(ctx, orgID, service.CreateNotificationInput{
			Name:   in.Body.Name,
			Type:   in.Body.Type,
			Config: in.Body.Config,
			Events: in.Body.Events,
		})
		if err != nil {
			return nil, huma.Error400BadRequest(err.Error(), err)
		}
		return &struct{ Body *meshdb.NotificationChannel }{Body: row}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "update-notification-channel",
		Method:      "PUT",
		Path:        "/api/v1/orgs/{orgId}/notification-channels/{id}",
		Tags:        []string{"notifications"},
	}, func(ctx context.Context, in *struct {
		OrgID string `path:"orgId"`
		ID    string `path:"id"`
		Body  struct {
			Name    *string           `json:"name,omitempty"`
			Config  map[string]string `json:"config,omitempty"`
			Events  []string          `json:"events,omitempty"`
			Enabled *bool             `json:"enabled,omitempty"`
		}
	}) (*struct{ Body *meshdb.NotificationChannel }, error) {
		_, orgID, id, err := h.checkOrgAdminAccess(ctx, in.OrgID, in.ID)
		if err != nil {
			return nil, err
		}
		row, err := h.svc.Notifications.Update(ctx, id, orgID, service.UpdateNotificationInput{
			Name:    in.Body.Name,
			Config:  in.Body.Config,
			Events:  in.Body.Events,
			Enabled: in.Body.Enabled,
		})
		if err != nil {
			return nil, err
		}
		return &struct{ Body *meshdb.NotificationChannel }{Body: row}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "delete-notification-channel",
		Method:      "DELETE",
		Path:        "/api/v1/orgs/{orgId}/notification-channels/{id}",
		Tags:        []string{"notifications"},
	}, func(ctx context.Context, in *struct {
		OrgID string `path:"orgId"`
		ID    string `path:"id"`
	}) (*struct{}, error) {
		_, orgID, id, err := h.checkOrgAdminAccess(ctx, in.OrgID, in.ID)
		if err != nil {
			return nil, err
		}
		return nil, h.svc.Notifications.Delete(ctx, id, orgID)
	})
}

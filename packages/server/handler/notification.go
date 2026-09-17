package handler

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/meshploy/packages/server/service"
	meshdb "github.com/meshploy/packages/db"
)

func (h *Handler) registerNotificationRoutes(api huma.API) {
	// The events a channel can subscribe to, served rather than written out
	// again in the console: a hand-kept second list is how job.failed came to
	// be dispatched for months with no way to subscribe to it.
	huma.Register(api, huma.Operation{
		OperationID: "list-notification-events",
		Method:      "GET",
		Path:        "/api/v1/notification-events",
		Summary:     "List the events a notification channel can subscribe to",
		Tags:        []string{"notifications"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, func(ctx context.Context, _ *struct{}) (*struct{ Body []service.EventDef }, error) {
		if _, err := requireUser(ctx); err != nil {
			return nil, err
		}
		return &struct{ Body []service.EventDef }{Body: service.Events()}, nil
	})

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

	huma.Register(api, huma.Operation{
		OperationID: "test-notification-channel",
		Method:      "POST",
		Path:        "/api/v1/orgs/{orgId}/notification-channels/{id}/test",
		Summary:     "Send a test notification to a channel",
		Description: "Sends at once and records the attempt. A failed send is a result, not an error: the body says what went wrong.",
		Tags:        []string{"notifications"},
	}, func(ctx context.Context, in *struct {
		OrgID string `path:"orgId"`
		ID    string `path:"id"`
	}) (*struct{ Body *meshdb.NotificationDelivery }, error) {
		_, orgID, id, err := h.checkOrgAdminAccess(ctx, in.OrgID, in.ID)
		if err != nil {
			return nil, err
		}
		row, err := h.svc.Notifications.Test(ctx, orgID, id)
		if err != nil {
			return nil, notFound(err)
		}
		return &struct{ Body *meshdb.NotificationDelivery }{Body: row}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "list-notification-deliveries",
		Method:      "GET",
		Path:        "/api/v1/orgs/{orgId}/notification-channels/{id}/deliveries",
		Summary:     "List a channel's delivery attempts, newest first",
		Tags:        []string{"notifications"},
	}, func(ctx context.Context, in *struct {
		OrgID  string `path:"orgId"`
		ID     string `path:"id"`
		Status string `query:"status" enum:"all,failed" default:"all"`
		Limit  int    `query:"limit" minimum:"1" maximum:"200" default:"50"`
	}) (*struct{ Body []meshdb.NotificationDelivery }, error) {
		_, orgID, id, err := h.checkOrgMemberAccess(ctx, in.OrgID, in.ID)
		if err != nil {
			return nil, err
		}
		rows, err := h.svc.Notifications.Deliveries(ctx, orgID, id, in.Status == "failed", in.Limit)
		if err != nil {
			return nil, notFound(err)
		}
		return &struct{ Body []meshdb.NotificationDelivery }{Body: rows}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "retry-notification-delivery",
		Method:      "POST",
		Path:        "/api/v1/orgs/{orgId}/notification-deliveries/{id}/retry",
		Summary:     "Send a recorded delivery again",
		Description: "Resends the event with the data it carried, to the channel as it is configured now, and records a new attempt.",
		Tags:        []string{"notifications"},
	}, func(ctx context.Context, in *struct {
		OrgID string `path:"orgId"`
		ID    string `path:"id"`
	}) (*struct{ Body *meshdb.NotificationDelivery }, error) {
		_, orgID, id, err := h.checkOrgAdminAccess(ctx, in.OrgID, in.ID)
		if err != nil {
			return nil, err
		}
		row, err := h.svc.Notifications.Retry(ctx, orgID, id)
		if err != nil {
			return nil, notFound(err)
		}
		return &struct{ Body *meshdb.NotificationDelivery }{Body: row}, nil
	})
}

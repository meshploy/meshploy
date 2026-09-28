package handler

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/meshploy/packages/db"
)

// Services made before a service kept its last images from the start keep
// every image they build. A level's list of them, and the one step that moves
// them all to keeping their last images.

type ImageRetentionOutput struct {
	Body struct {
		// Services is who keeps every image, by name.
		Services []ImageRetentionService `json:"services"`
		Keep     int                     `json:"keep" doc:"How many images each would keep"`
	}
}

type ImageRetentionService struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	ImagesKept int    `json:"images_kept"`
}

func (h *Handler) registerImageRetention(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "list-services-keeping-every-image",
		Method:      "GET",
		Path:        "/api/v1/orgs/{orgId}/projects/{projectId}/image-retention",
		Summary:     "Services of a level that keep every image they build",
		Tags:        []string{"Services"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.ListKeepingEveryImage)

	huma.Register(api, huma.Operation{
		OperationID: "keep-last-images",
		Method:      "POST",
		Path:        "/api/v1/orgs/{orgId}/projects/{projectId}/image-retention",
		Summary:     "Make every service of a level that keeps every image keep its last images, and remove the rest",
		Tags:        []string{"Services"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.KeepLastImages)
}

func (h *Handler) ListKeepingEveryImage(ctx context.Context, input *ProjectPathInput) (*ImageRetentionOutput, error) {
	_, _, projectID, _, err := h.checkAccess(ctx, input.OrgID, input.ProjectID, db.ResourceProject, db.ActionView, "")
	if err != nil {
		return nil, err
	}
	svcs, err := h.svc.Workloads.KeepingEveryImage(ctx, projectID)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return h.imageRetentionOutput(ctx, svcs), nil
}

func (h *Handler) KeepLastImages(ctx context.Context, input *ProjectPathInput) (*ImageRetentionOutput, error) {
	_, _, projectID, _, err := h.checkAccess(ctx, input.OrgID, input.ProjectID, db.ResourceProject, db.ActionUpdate, "")
	if err != nil {
		return nil, err
	}
	svcs, err := h.svc.Workloads.KeepLastImages(ctx, projectID)
	if err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	return h.imageRetentionOutput(ctx, svcs), nil
}

func (h *Handler) imageRetentionOutput(ctx context.Context, svcs []db.Service) *ImageRetentionOutput {
	out := &ImageRetentionOutput{}
	out.Body.Keep = db.DefaultImageRetention
	out.Body.Services = make([]ImageRetentionService, 0, len(svcs))
	for _, s := range svcs {
		out.Body.Services = append(out.Body.Services, ImageRetentionService{
			ID: s.ID.String(), Name: s.Name, ImagesKept: h.svc.Deployments.ImagesKept(ctx, s.ID),
		})
	}
	return out
}

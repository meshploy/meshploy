package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/go-chi/chi/v5"
	"github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/service"
)

// uploadSourceResponse is what an upload answers: what was stored, and the
// deployment it started when asked to deploy.
type uploadSourceResponse struct {
	Source     *service.UploadedSource `json:"source"`
	Deployment *db.Deployment          `json:"deployment,omitempty"`
}

// UploadServiceSource takes a folder, packed as a tar.gz in the request body,
// as the service's source: POST .../services/{serviceId}/source?name=my-app
// &deploy=true. Raw rather than a Huma operation so the archive streams to
// disk instead of being held in memory. Replacing what a service is built from
// is deploying it, so it needs the deploy permission.
func (h *Handler) UploadServiceSource(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID, _, serviceID, _, err := h.checkAccess(ctx, chi.URLParam(r, "orgId"), chi.URLParam(r, "serviceId"),
		db.ResourceService, db.ActionDeploy, chi.URLParam(r, "projectId"))
	if err != nil {
		writeRawError(w, err)
		return
	}
	body := http.MaxBytesReader(w, r.Body, service.MaxUploadBytes)
	src, err := h.svc.Deployments.UploadSource(ctx, serviceID, r.URL.Query().Get("name"), body)
	switch {
	case errors.Is(err, service.ErrUploadTooLarge):
		writeRawError(w, huma.NewError(http.StatusRequestEntityTooLarge, err.Error()))
		return
	case errors.Is(err, service.ErrUploadInvalid), errors.Is(err, service.ErrUploadNotApplication):
		writeRawError(w, huma.Error400BadRequest(err.Error()))
		return
	case err != nil:
		writeRawError(w, huma.Error502BadGateway(err.Error()))
		return
	}

	out := uploadSourceResponse{Source: src}
	if r.URL.Query().Get("deploy") == "true" {
		dep, err := h.svc.Deployments.Trigger(ctx, service.TriggerInput{ServiceID: serviceID, TriggeredBy: userID})
		if err != nil {
			// Stored, and not deployed: said, so the caller can deploy later.
			writeRawError(w, huma.Error400BadRequest("the folder is stored, but the deploy did not start: "+err.Error()))
			return
		}
		out.Deployment = dep
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(out)
}

// writeRawError answers a raw handler's error the way Huma answers its own.
func writeRawError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	var se huma.StatusError
	if errors.As(err, &se) {
		status = se.GetStatus()
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"status": status, "title": http.StatusText(status), "detail": errMessage(err)})
}

func errMessage(err error) string {
	var em *huma.ErrorModel
	if errors.As(err, &em) {
		return em.Detail
	}
	return err.Error()
}

package handler

import (
	"context"
	"time"

	"github.com/danielgtaylor/huma/v2"
)

type HealthOutput struct {
	Body struct {
		Status  string `json:"status"`
		Time    string `json:"time"`
		DB      string `json:"db"`
	}
}

func (h *Handler) registerHealthRoute(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "health",
		Method:      "GET",
		Path:        "/health",
		Summary:     "Health check",
		Tags:        []string{"System"},
	}, h.Health)
}

func (h *Handler) Health(ctx context.Context, _ *struct{}) (*HealthOutput, error) {
	out := &HealthOutput{}
	out.Body.Status = "ok"
	out.Body.Time = time.Now().UTC().Format(time.RFC3339)

	// Ping the database — surface connectivity problems early.
	if err := h.svc.System.Ping(ctx); err != nil {
		out.Body.Status = "degraded"
		out.Body.DB = err.Error()
	} else {
		out.Body.DB = "ok"
	}

	return out, nil
}

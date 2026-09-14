package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
)

// A stack update takes only what changes: the variables page sends only
// variables, and the sync banner only git_mode. What is left out reaches the
// service as nil, so the stack keeps it.
func TestStackUpdateTakesPartialBodies(t *testing.T) {
	_, api := humatest.New(t)
	var got *UpdateStackInput
	huma.Register(api, huma.Operation{OperationID: "update-stack", Method: http.MethodPut, Path: "/stacks/{orgId}/{projectId}/{stackId}"},
		func(_ context.Context, in *UpdateStackInput) (*struct{}, error) {
			got = in
			return nil, nil
		})

	for _, body := range []map[string]any{
		{"git_mode": "repo"},
		{"variables": map[string]string{"POSTGRES_USER": "app"}},
	} {
		got = nil
		resp := api.Put("/stacks/o/p/s", body)
		if resp.Code >= 300 || got == nil {
			t.Fatalf("%v: %d %s", body, resp.Code, resp.Body.String())
		}
		b := got.Body
		if b.Spec != nil || b.GitRepo != nil || b.GitBranch != nil || b.GitPath != nil {
			t.Errorf("%v: fields left out arrived set: %+v", body, b)
		}
		if _, sent := body["git_mode"]; sent != (b.GitMode != nil) {
			t.Errorf("%v: git_mode arrived as %v", body, b.GitMode)
		}
	}
}

package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
)

// A stack from git sends no spec: it has none until it syncs. The console's
// body is exactly this, and was refused as "expected required property spec".
func TestAGitStackNeedsNoSpec(t *testing.T) {
	_, api := humatest.New(t)
	var got *CreateStackInput
	huma.Register(api, huma.Operation{OperationID: "create-stack", Method: http.MethodPost, Path: "/stacks/{orgId}/{projectId}"},
		func(_ context.Context, in *CreateStackInput) (*struct{}, error) {
			got = in
			return nil, nil
		})
	resp := api.Post("/stacks/o/p", map[string]any{
		"name": "powerinsight", "git_mode": "repo", "git_repo": "acme/app",
		"git_branch": "main", "git_path": "docker-compose.yml", "git_integration_id": "3ce839ec-4396-4eb2-9dcc-5613ffc76d48",
	})
	if resp.Code >= 300 {
		t.Fatalf("refused: %d %s", resp.Code, resp.Body.String())
	}
	if got == nil || got.Body.GitRepo != "acme/app" || got.Body.Spec != "" {
		t.Fatalf("body %+v", got)
	}
}

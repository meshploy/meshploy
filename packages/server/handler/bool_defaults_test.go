package handler

import (
	"context"
	"maps"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
)

// enabled and use_tls left out of a request mean true, and an explicit false
// stays false. They are pointers because huma fills every zero field with its
// default, so a plain bool defaulting to true could never be turned off.
func TestBoolFieldsDefaultToTrueOnlyWhenLeftOut(t *testing.T) {
	_, api := humatest.New(t)
	var got []bool
	capture := func(b *bool) { got = append(got, b == nil || *b) }

	huma.Register(api, huma.Operation{OperationID: "system-backup", Method: http.MethodPut, Path: "/system/{orgId}"},
		func(_ context.Context, in *UpsertSystemBackupInput) (*struct{}, error) {
			capture(in.Body.Enabled)
			return nil, nil
		})
	huma.Register(api, huma.Operation{OperationID: "volume-backup", Method: http.MethodPut, Path: "/volume/{orgId}/{projectId}/{volumeId}"},
		func(_ context.Context, in *UpsertVolumeBackupInput) (*struct{}, error) {
			capture(in.Body.Enabled)
			return nil, nil
		})
	huma.Register(api, huma.Operation{OperationID: "email-config", Method: http.MethodPut, Path: "/email/{orgId}"},
		func(_ context.Context, in *SaveEmailConfigInput) (*struct{}, error) {
			capture(in.Body.UseTLS)
			return nil, nil
		})

	endpoints := []struct {
		path  string
		body  map[string]any
		field string
	}{
		{"/system/o", map[string]any{"storage_integration_id": "s", "schedule": "0 2 * * *"}, "enabled"},
		{"/volume/o/p/v", map[string]any{"storage_integration_id": "s", "schedule": "0 2 * * *", "retention_days": 7}, "enabled"},
		{"/email/o", map[string]any{
			"host": "smtp.example.com", "port": 587, "username": "ops", "password": "pw",
			"from_address": "ops@example.com", "from_name": "Ops",
		}, "use_tls"},
	}
	for _, e := range endpoints {
		for _, tc := range []struct {
			sent any
			want bool
		}{{nil, true}, {false, false}, {true, true}} {
			body := maps.Clone(e.body)
			if tc.sent != nil {
				body[e.field] = tc.sent
			}
			got = nil
			resp := api.Put(e.path, body)
			if resp.Code >= 300 {
				t.Fatalf("%s with %s=%v: %d %s", e.path, e.field, tc.sent, resp.Code, resp.Body.String())
			}
			if len(got) != 1 || got[0] != tc.want {
				t.Errorf("%s with %s=%v: got %v, want %v", e.path, e.field, tc.sent, got, tc.want)
			}
		}
	}
}

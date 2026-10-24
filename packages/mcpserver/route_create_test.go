package mcpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/meshploy/packages/client"
)

// createStub answers a route list holding the apex route, and records the body
// a route create sends.
func createStub(t *testing.T, sent *map[string]any) *srv {
	t.Helper()
	h := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/routes"):
			_ = json.NewEncoder(w).Encode([]map[string]any{{"id": "apex-id", "hostname": "example.com"}})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/routes"):
			_ = json.NewDecoder(r.Body).Decode(sent)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "new", "hostname": (*sent)["hostname"], "domain_id": nil})
		case strings.HasSuffix(r.URL.Path, "/nodes"):
			_ = json.NewEncoder(w).Encode([]map[string]any{})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(h.Close)
	return &srv{c: client.New(h.URL, "t"), orgID: "org-1"}
}

func createReq(args map[string]any) mcp.CallToolRequest {
	var req mcp.CallToolRequest
	args["project_id"] = "p1"
	req.Params.Arguments = args
	return req
}

func sentTarget(t *testing.T, sent map[string]any) map[string]any {
	t.Helper()
	targets, _ := sent["targets"].([]any)
	if len(targets) != 1 {
		t.Fatalf("want one target, sent %v", sent)
	}
	return targets[0].(map[string]any)
}

// A www name redirects to the apex by naming its hostname; the API is given the
// apex route's ID and no service.
func TestCreateRouteRedirectsToARouteByHostname(t *testing.T) {
	var sent map[string]any
	s := createStub(t, &sent)
	res, err := s.handleCreateRoute(context.Background(), createReq(map[string]any{
		"hostname": "www.example.com", "redirect_to": "example.com", "redirect_code": float64(302),
	}))
	if err != nil || res.IsError {
		t.Fatalf("create failed: %v %s", err, resultText(t, res))
	}
	tg := sentTarget(t, sent)
	if tg["redirect_route_id"] != "apex-id" || tg["redirect_code"] != float64(302) {
		t.Fatalf("redirect not sent: %v", tg)
	}
	if _, ok := tg["service_id"]; ok {
		t.Fatalf("a redirect sent a service: %v", tg)
	}
}

func TestCreateRouteToAServiceSendsNoRedirect(t *testing.T) {
	var sent map[string]any
	s := createStub(t, &sent)
	res, _ := s.handleCreateRoute(context.Background(), createReq(map[string]any{
		"hostname": "app.example.com", "service_id": "svc-1",
	}))
	if res.IsError {
		t.Fatalf("create failed: %s", resultText(t, res))
	}
	tg := sentTarget(t, sent)
	if tg["service_id"] != "svc-1" {
		t.Fatalf("service not sent: %v", tg)
	}
	if _, ok := tg["redirect_route_id"]; ok {
		t.Fatalf("a service route sent a redirect: %v", tg)
	}
}

func TestCreateRouteRefusesAmbiguousTargets(t *testing.T) {
	cases := map[string]map[string]any{
		"neither":          {"hostname": "a.example.com"},
		"both":             {"hostname": "a.example.com", "service_id": "svc-1", "redirect_to": "example.com"},
		"code on service":  {"hostname": "a.example.com", "service_id": "svc-1", "redirect_code": float64(301)},
		"bad code":         {"hostname": "a.example.com", "redirect_to": "example.com", "redirect_code": float64(307)},
		"unknown hostname": {"hostname": "a.example.com", "redirect_to": "other.example"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			var sent map[string]any
			s := createStub(t, &sent)
			res, _ := s.handleCreateRoute(context.Background(), createReq(args))
			if !res.IsError {
				t.Fatalf("accepted: %v", args)
			}
			if sent != nil {
				t.Fatalf("a refused create reached the API: %v", sent)
			}
		})
	}
}

package mcpserver

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/meshploy/packages/client"
)

// Registration is the silent failure: the server starts and the agent simply
// cannot see the tool.
func TestVerifyRouteHostnameIsRegistered(t *testing.T) {
	saved := toolHooks
	t.Cleanup(func() { toolHooks = saved })
	toolHooks = nil

	ms := New(client.New("http://127.0.0.1:0", "t"), "org-1")
	res := ms.HandleMessage(context.Background(), json.RawMessage(
		`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	b, _ := json.Marshal(res)
	if !strings.Contains(string(b), `"verify_route_hostname"`) {
		t.Fatal("verify_route_hostname is not on the MCP surface")
	}
}

// routeStub answers the endpoints the route tools touch. found decides whether
// the TXT record has been published yet.
func routeStub(t *testing.T, route map[string]any, found *bool) *srv {
	t.Helper()
	h := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/routes"):
			_ = json.NewEncoder(w).Encode([]map[string]any{route})
		case strings.HasSuffix(r.URL.Path, "/nodes"):
			_ = json.NewEncoder(w).Encode([]map[string]any{{"id": "n1", "k3s_role": "server", "public_ip": "203.0.113.10"}})
		case strings.HasSuffix(r.URL.Path, "/verify-hostname"):
			if !*found {
				w.WriteHeader(http.StatusUnprocessableEntity)
				_, _ = io.WriteString(w, `{"detail":"TXT record not found or not yet propagated"}`)
				return
			}
			route["custom_domain_verified"] = true
			_ = json.NewEncoder(w).Encode(route)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(h.Close)
	return &srv{c: client.New(h.URL, "t"), orgID: "org-1"}
}

func unprovedRoute() map[string]any {
	return map[string]any{
		"id": "r1", "hostname": "store.customer.example", "domain_id": nil,
		"custom_domain_verified": false, "custom_domain_verify_token": "7d2e9a0c",
	}
}

func verifyReq(routeID string) mcp.CallToolRequest {
	var req mcp.CallToolRequest
	req.Params.Arguments = map[string]any{"project_id": "p1", "route_id": routeID}
	return req
}

func resultText(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	b, _ := json.Marshal(res)
	return string(b)
}

// An agent reading a route learns whether it will work, and exactly what the
// user has to add if not - without a second call.
func TestRouteResultCarriesTheRecordsToProve(t *testing.T) {
	found := false
	s := routeStub(t, unprovedRoute(), &found)

	var r client.Route
	b, _ := json.Marshal(unprovedRoute())
	_ = json.Unmarshal(b, &r)
	out := s.toMCPRoute(r, "")

	if !out.CustomHostname || out.OwnershipVerified {
		t.Fatalf("a new custom hostname is unproved: %+v", out)
	}
	if len(out.ProveOwnership) != 2 {
		t.Fatalf("want the TXT and the A record, got %+v", out.ProveOwnership)
	}
	if out.ProveOwnership[0] != (MCPDNSRecord{Name: "_meshploy-verify.store.customer.example", Type: "TXT", Value: "7d2e9a0c"}) {
		t.Errorf("TXT record = %+v", out.ProveOwnership[0])
	}
	if out.ProveOwnership[1].Value != "203.0.113.10" {
		t.Errorf("the A record should point at the gateway, got %+v", out.ProveOwnership[1])
	}

	// Proved, or on a base domain: nothing to add, and the field is absent.
	sub := client.Route{ID: "r2", Hostname: "shop.example.com", DomainID: new(string)}
	if got := s.toMCPRoute(sub, "x"); got.CustomHostname || !got.OwnershipVerified || got.ProveOwnership != nil {
		t.Errorf("a base-domain route has nothing to prove: %+v", got)
	}
}

func TestVerifyRouteHostnameNotFoundNamesTheRecords(t *testing.T) {
	found := false
	s := routeStub(t, unprovedRoute(), &found)

	res, err := s.handleVerifyRouteHostname(context.Background(), verifyReq("store.customer.example"))
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("an unproved hostname must be an error result")
	}
	text := resultText(t, res)
	for _, want := range []string{"not yet propagated", "_meshploy-verify.store.customer.example TXT 7d2e9a0c", "store.customer.example A 203.0.113.10"} {
		if !strings.Contains(text, want) {
			t.Errorf("result lacks %q: %s", want, text)
		}
	}
}

func TestVerifyRouteHostnameOnceFound(t *testing.T) {
	found := true
	s := routeStub(t, unprovedRoute(), &found)

	res, err := s.handleVerifyRouteHostname(context.Background(), verifyReq("r1"))
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("want success, got %s", resultText(t, res))
	}
	if text := resultText(t, res); !strings.Contains(text, `\"ownership_verified\":true`) {
		t.Errorf("result should report the hostname verified: %s", text)
	}
}

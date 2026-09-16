package mcpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/meshploy/packages/client"
)

// Registration is one call that is easy to omit when adding a tool group, and
// the failure is silent: the server starts fine and an agent simply cannot see
// the templates it is meant to deploy from.
func TestTemplateToolsAreRegistered(t *testing.T) {
	saved := toolHooks
	t.Cleanup(func() { toolHooks = saved })
	toolHooks = nil

	ms := New(client.New("http://127.0.0.1:0", "t"), "org-1")
	res := ms.HandleMessage(context.Background(), json.RawMessage(
		`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	b, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal tools/list: %v", err)
	}
	for _, name := range []string{"list_templates", "get_template", "deploy_template"} {
		if !strings.Contains(string(b), `"`+name+`"`) {
			t.Errorf("tool %q is not on the MCP surface", name)
		}
	}
}

// What a template asks for is declared; what it is answered with is a secret.
// The tools must pass the answers through and report back only the stack.
func TestDeployTemplateSendsPromptValues(t *testing.T) {
	var body map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/orgs/org-1/projects/p1/templates/zot/deploy", func(w http.ResponseWriter, r *http.Request) {
		record(t, r, &body)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(client.Stack{ID: "st1", Name: "zot", Status: "deploying"})
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	s := &srv{c: client.New(ts.URL, "t"), orgID: "org-1"}

	res := callTool(t, s.handleDeployTemplate, map[string]any{
		"project_id": "p1", "template_id": "zot",
		"prompt_values": "ADMIN_PASSWORD=s3cr3t",
	})
	if res.IsError {
		t.Fatalf("deploy failed: %+v", res.Content)
	}
	values, _ := body["prompt_values"].(map[string]any)
	if values["ADMIN_PASSWORD"] != "s3cr3t" {
		t.Errorf("prompt values not sent: %v", body)
	}
	out, _ := json.Marshal(res)
	if strings.Contains(string(out), "s3cr3t") {
		t.Error("the tool result echoes a secret back to the agent")
	}
}

// A template's variables are declarations. A value field would be silently
// empty, which reads as "no password" rather than "never shown".
func TestMCPTemplateCarriesNoValues(t *testing.T) {
	b, err := json.Marshal(toMCPTemplate(client.Template{
		ID: "zot", Name: "Zot", Variables: []client.TemplateVariable{
			{Key: "ADMIN_PASSWORD", Generate: "password"},
			{Key: "ADMIN_USER", Prompt: "Admin username", Required: true},
		},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(string(b)), `"value"`) {
		t.Fatalf("agent-facing template exposes a value: %s", b)
	}
	if !strings.Contains(string(b), `"generate":"password"`) {
		t.Errorf("a generated variable should say so: %s", b)
	}
}

// The TCP tools are the agent's half of publishing a port, and registration is
// the silent failure: the server starts and the agent simply cannot see them.
func TestTCPPortToolsAreRegistered(t *testing.T) {
	saved := toolHooks
	t.Cleanup(func() { toolHooks = saved })
	toolHooks = nil

	ms := New(client.New("http://127.0.0.1:0", "t"), "org-1")
	res := ms.HandleMessage(context.Background(), json.RawMessage(
		`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	b, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal tools/list: %v", err)
	}
	for _, name := range []string{"list_tcp_ports", "publish_tcp_port", "unpublish_tcp_port"} {
		if !strings.Contains(string(b), `"`+name+`"`) {
			t.Errorf("tool %q is not on the MCP surface", name)
		}
	}
}

// A published port with no allow-list is open to the internet, so the agent has
// to see that plainly rather than as an absent field.
func TestMCPTCPRouteReportsWhoMayConnect(t *testing.T) {
	b, err := json.Marshal(toMCPTCPRoute(client.TCPRoute{
		ID: "r1", GatewayPort: 5432, TargetIP: "100.64.0.1", TargetPort: 31432, Status: "open",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"allowed_from":[]`) {
		t.Errorf("an open port should say so as an empty list, got %s", b)
	}
	if !strings.Contains(string(b), `"target":"100.64.0.1:31432"`) {
		t.Errorf("target not reported: %s", b)
	}
}

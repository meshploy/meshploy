package agent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestRegisterSendsTheIdentityAndKeepsTheSecret(t *testing.T) {
	id := Identity{Dir: t.TempDir()}
	if _, err := id.Ensure(); err != nil {
		t.Fatal(err)
	}
	cert, _ := id.CertPEM()
	token, _ := id.Token()
	var got map[string]any
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/nodes/self-register" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		json.NewDecoder(r.Body).Decode(&got)
		w.Write([]byte(`{"id":"node-1","node_secret":"mnode-secret"}`))
	}))
	defer api.Close()

	res, err := Register(t.Context(), api.URL+"/", id, Registration{Token: "mprov-x", Name: "edge", MeshIP: "100.64.0.2", Port: 7443})
	if err != nil {
		t.Fatal(err)
	}
	if res.NodeID != "node-1" {
		t.Errorf("node %q", res.NodeID)
	}
	agent, _ := got["cluster_agent"].(map[string]any)
	if got["token"] != "mprov-x" || got["tailscale_ip"] != "100.64.0.2" || agent["certificate"] != string(cert) || agent["token"] != token || agent["port"] != float64(7443) {
		t.Errorf("sent %v", got)
	}
	st, err := os.Stat(id.NodeSecretPath())
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("node secret file: %v %v", err, st)
	}
	if raw, _ := os.ReadFile(id.NodeSecretPath()); strings.TrimSpace(string(raw)) != "mnode-secret" {
		t.Error("node secret not kept")
	}
}

func TestRegisterSaysWhyItWasRefused(t *testing.T) {
	id := Identity{Dir: t.TempDir()}
	id.Ensure()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusConflict)
		w.Write([]byte(`{"status":409,"detail":"a cluster agent is already registered; revoke it first"}`))
	}))
	defer api.Close()
	_, err := Register(t.Context(), api.URL, id, Registration{Token: "mprov-x", Name: "edge", MeshIP: "100.64.0.2", Port: 7443})
	if err == nil || !strings.Contains(err.Error(), "already registered") || !strings.Contains(err.Error(), "409") {
		t.Fatalf("got %v", err)
	}
	if _, err := os.Stat(id.NodeSecretPath()); err == nil {
		t.Error("a refused registration wrote a node secret")
	}
}

package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/meshploy/apps/cli/internal/config"
)

const (
	verifyTestOrg     = "00000000-0000-0000-0000-000000000001"
	verifyTestProject = "00000000-0000-0000-0000-000000000003"
)

// verifyAPI is a stub of the three endpoints `route verify` touches. found
// decides whether the TXT record is there yet.
func verifyAPI(t *testing.T, route map[string]any, found *bool) {
	t.Helper()
	base := "/api/v1/orgs/" + verifyTestOrg
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == base+"/projects/"+verifyTestProject+"/routes":
			_ = json.NewEncoder(w).Encode([]map[string]any{route})
		case r.URL.Path == base+"/nodes":
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
	t.Cleanup(srv.Close)

	saved, savedProject := loadedCfg, routeProject
	loadedCfg = &config.Config{APIURL: srv.URL, Token: "t", OrgID: verifyTestOrg}
	routeProject = verifyTestProject
	t.Cleanup(func() { loadedCfg, routeProject = saved, savedProject })
}

func runVerify(t *testing.T, ref string) (string, error) {
	t.Helper()
	r, w, _ := os.Pipe()
	saved := os.Stdout
	os.Stdout = w
	err := routeVerifyCmd.RunE(routeVerifyCmd, []string{ref})
	os.Stdout = saved
	w.Close()
	out, _ := io.ReadAll(r)
	return string(out), err
}

func customRoute() map[string]any {
	return map[string]any{
		"id": "r1", "hostname": "store.customer.example", "domain_id": nil,
		"custom_domain_verified": false, "custom_domain_verify_token": "7d2e9a0c",
	}
}

// Not found yet: the command fails, and prints exactly what to add, so the
// person running it is never left guessing why there is no certificate.
func TestRouteVerifyPrintsTheRecordsWhenNotFound(t *testing.T) {
	found := false
	verifyAPI(t, customRoute(), &found)

	out, err := runVerify(t, "store.customer.example")
	if err == nil {
		t.Fatal("an unproved hostname must fail the command")
	}
	for _, want := range []string{"_meshploy-verify.store.customer.example", "TXT", "7d2e9a0c", "203.0.113.10", "meshploy route verify r1"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if !strings.Contains(err.Error(), "propagate") {
		t.Errorf("the error should say DNS can lag: %v", err)
	}
}

func TestRouteVerifySucceedsOnceTheRecordIsFound(t *testing.T) {
	found := true
	verifyAPI(t, customRoute(), &found)

	out, err := runVerify(t, "r1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "is verified") {
		t.Fatalf("got:\n%s", out)
	}
}

// A subdomain of a base domain was proved with the zone. Asking the API would
// only earn an error about something that is already fine.
func TestRouteVerifyOnABaseDomainHasNothingToDo(t *testing.T) {
	found := false
	route := map[string]any{"id": "r2", "hostname": "shop.example.com", "domain_id": "d1"}
	verifyAPI(t, route, &found)

	out, err := runVerify(t, "r2")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "already proved") {
		t.Fatalf("got:\n%s", out)
	}
}

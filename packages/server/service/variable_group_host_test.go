package service

import (
	"testing"

	"github.com/google/uuid"
	db "github.com/meshploy/packages/db"
)

// Service-discovery hostnames must be the K8s Service name. The service name
// itself is not one: it may contain spaces or capitals, which are not legal in
// a DNS label, and a database's workload carries a suffixed slug. A hostname
// that does not exist in the cluster does not fail loudly either — it falls
// through to the mesh search domain and resolves to the gateway.
func TestServiceDiscoveryHostIsADNSName(t *testing.T) {
	cases := []struct {
		name     string
		wantHost string
	}{
		{"umami-db", "umami-db.proj.svc.cluster.local"},
		{"My DB", "my-db.proj.svc.cluster.local"},
		{"Auth_UI", "auth-ui.proj.svc.cluster.local"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := serviceDNSName(tc.name, "proj")
			if got != tc.wantHost {
				t.Errorf("host = %q, want %q", got, tc.wantHost)
			}
			for _, r := range got {
				if r >= 'A' && r <= 'Z' || r == ' ' || r == '_' {
					t.Errorf("host %q contains %q, which is not legal in a DNS label", got, r)
				}
			}
		})
	}
}

// The generated keys still come from the service name, so a rename of the
// hostname source must not change what other services read.
func TestServiceEnvPrefixUnchanged(t *testing.T) {
	svc := &db.Service{Base: db.Base{ID: uuid.New()}, Name: "umami-db"}
	if got, want := serviceEnvPrefix(svc.Name), "UMAMI_DB"; got != want {
		t.Errorf("prefix = %q, want %q", got, want)
	}
}

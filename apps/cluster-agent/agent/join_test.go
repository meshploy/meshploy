package agent

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	appk8s "github.com/meshploy/packages/server/k8s"
)

const fakeJoin = "K10" + "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef" + "::abc123.0123456789abcdef"

// A join token is minted by the agent, for an hour whatever the API would
// like, named for the joining node, and only in the shape k3s gives.
func TestTheAgentMintsShortJoinTokens(t *testing.T) {
	api := newAPIServer(t)
	a, _, _ := testAgent(t, api)
	addr, id := servedAgent(t, a)
	c, _ := appk8s.NewAgentCluster()
	certPEM, _ := id.CertPEM()
	token, _ := id.Token()
	if err := c.Set(appk8s.AgentIdentity{Addr: addr, CertPEM: certPEM, Token: token}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	var refusal *appk8s.AgentRefusal

	if _, err := c.JoinToken(ctx, "worker-1"); !errors.As(err, &refusal) || refusal.Status != http.StatusForbidden {
		t.Fatalf("an agent not set up to mint: %v", err)
	}

	var gotTTL time.Duration
	var gotDesc string
	minted := fakeJoin
	a.mintJoin = func(_ context.Context, ttl time.Duration, desc string) (string, error) {
		gotTTL, gotDesc = ttl, desc
		return minted, nil
	}
	j, err := c.JoinToken(ctx, "worker-1")
	if err != nil || j.Token != fakeJoin {
		t.Fatalf("join token: %+v %v", j, err)
	}
	if gotTTL != JoinTokenTTL || !strings.Contains(gotDesc, "worker-1") {
		t.Errorf("minted for %s as %q", gotTTL, gotDesc)
	}
	if left := time.Until(j.ExpiresAt); left > JoinTokenTTL || left < JoinTokenTTL-time.Minute {
		t.Errorf("expires in %s", left)
	}

	if _, err := c.JoinToken(ctx, "worker 1; rm -rf /"); !errors.As(err, &refusal) {
		t.Errorf("a name that is not one: %v", err)
	}
	minted = "K10abc::the-server-token"
	if _, err := c.JoinToken(ctx, "worker-2"); err == nil {
		t.Error("a token of another shape was handed back")
	}
}

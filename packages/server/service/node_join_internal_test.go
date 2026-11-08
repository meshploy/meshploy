package service

import (
	"context"
	"os"
	"testing"
)

// On a gateway a joining machine gets the server's own token, from the
// config when the API cannot read the server's file, and the gateway's
// address; it never expires.
func TestAGatewayHandsOutItsServersToken(t *testing.T) {
	if _, err := os.Stat(k3sTokenPath); err == nil {
		t.Skip("this machine runs a k3s server, whose token would be read instead")
	}
	j, err := (&NodeService{k3sToken: "K10server"}).ClusterJoin(context.Background(), "w")
	if err != nil || j.Token != "K10server" || j.ServerURL != k3sGatewayServerURL || j.ExpiresAt != nil {
		t.Fatalf("%+v %v", j, err)
	}
}

// A node's name reaches the agent as a label it accepts.
func TestJoinTokenNameIsALabel(t *testing.T) {
	for in, want := range map[string]string{
		"worker-1":        "worker-1",
		"Build Box.local": "build-box-local",
		"--":              "node",
		"":                "node",
		"x; rm -rf /":     "x-rm--rf",
	} {
		if got := joinTokenName(in); got != want {
			t.Errorf("joinTokenName(%q) = %q, want %q", in, got, want)
		}
	}
	long := joinTokenName("a234567890123456789012345678901234567890123456789012345678901234567890")
	if len(long) > 63 {
		t.Errorf("%q is longer than a label", long)
	}
}

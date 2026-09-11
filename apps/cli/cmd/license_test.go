package cmd

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/meshploy/packages/client"
)

func statusOutput(t *testing.T, e *client.Entitlements) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	saved := os.Stdout
	os.Stdout = w
	printEntitlements(e)
	os.Stdout = saved
	w.Close()
	out, _ := io.ReadAll(r)
	return string(out)
}

// A licence activated on the Community image is in force but grants nothing
// there. The status must say so and name the switch, or it reads as a working
// Enterprise install.
func TestStatusOfALicensedCommunityInstallNamesTheSwitch(t *testing.T) {
	ent := &client.Entitlements{Licensed: true, Tier: "enterprise", CanActivate: true, Edition: "community"}
	out := statusOutput(t, ent)
	if !strings.Contains(out, "Edition:   Community") || !strings.Contains(out, "server-upgrade --ee") {
		t.Fatalf("want Community and the switch command, got:\n%s", out)
	}

	ent.Edition = "enterprise"
	out = statusOutput(t, ent)
	if !strings.Contains(out, "Edition:   Enterprise") || strings.Contains(out, "server-upgrade --ee") {
		t.Fatalf("want Enterprise and no switch command, got:\n%s", out)
	}
}

// An API older than the edition field could only verify a licence on the
// Enterprise image, so for those, being able to activate means Enterprise.
func TestIsEnterpriseFallsBackToCanActivate(t *testing.T) {
	cases := []struct {
		edition     string
		canActivate bool
		want        bool
	}{
		{"", true, true},
		{"", false, false},
		{"community", true, false},
		{"enterprise", false, true},
	}
	for _, c := range cases {
		e := client.Entitlements{Edition: c.edition, CanActivate: c.canActivate}
		if got := e.IsEnterprise(); got != c.want {
			t.Errorf("edition %q, can_activate %v: got %v, want %v", c.edition, c.canActivate, got, c.want)
		}
	}
}

package setup

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// recorder captures the commands a PortGuard would run, so the rules can be
// asserted without touching the firewall of the machine running the tests.
type recorder struct {
	calls   []string
	outputs map[string]string
	fail    map[string]bool
}

func (r *recorder) run(_ context.Context, name string, args ...string) ([]byte, error) {
	key := name + " " + strings.Join(args, " ")
	r.calls = append(r.calls, key)
	if r.fail[key] {
		return nil, errors.New("command failed")
	}
	return []byte(r.outputs[key]), nil
}

func (r *recorder) has(want string) bool {
	for _, c := range r.calls {
		if c == want {
			return true
		}
	}
	return false
}

func guard(r *recorder, present ...string) PortGuard {
	set := map[string]bool{}
	for _, p := range present {
		set[p] = true
	}
	return PortGuard{
		Run: r.run,
		Lookup: func(name string) (string, error) {
			if set[name] {
				return "/usr/sbin/" + name, nil
			}
			return "", errors.New("not found")
		},
	}
}

// The installer opens 80, 443, 53 and 3478 and nothing else, so without this
// the setup page is unreachable on any host with an active firewall — which is
// most fresh cloud images.
func TestUFWPortIsOpenedAndClosed(t *testing.T) {
	r := &recorder{outputs: map[string]string{"ufw status": "Status: active"}}

	close, opened := guard(r, "ufw").Open(context.Background(), 9000)
	if !opened {
		t.Fatal("an active ufw should have had a rule added")
	}
	if !r.has("ufw allow 9000/tcp comment Meshploy setup") {
		t.Errorf("no allow rule was added: %v", r.calls)
	}

	close()
	if !r.has("ufw delete allow 9000/tcp") {
		t.Errorf("the rule was not removed on shutdown: %v", r.calls)
	}
}

// firewalld rules are added without --permanent: setup finishes long before the
// next reboot, and a rule that survives one is a port left open for good.
func TestFirewalldRuleIsNotPermanent(t *testing.T) {
	r := &recorder{outputs: map[string]string{"firewall-cmd --state": "running"}}

	close, opened := guard(r, "firewall-cmd").Open(context.Background(), 9000)
	if !opened {
		t.Fatal("a running firewalld should have had a rule added")
	}
	for _, c := range r.calls {
		if strings.Contains(c, "--permanent") {
			t.Errorf("the setup rule must not survive a reboot: %q", c)
		}
	}
	close()
	if !r.has("firewall-cmd --remove-port=9000/tcp") {
		t.Errorf("the rule was not removed: %v", r.calls)
	}
}

// An installed but inactive firewall needs no rule, and must not be started on
// the operator's behalf — that would change the host's security posture as a
// side effect of running an installer.
func TestInactiveFirewallIsLeftAlone(t *testing.T) {
	r := &recorder{outputs: map[string]string{"ufw status": "Status: inactive"}}

	_, opened := guard(r, "ufw").Open(context.Background(), 9000)
	if opened {
		t.Error("an inactive firewall should not have been touched")
	}
	for _, c := range r.calls {
		if strings.Contains(c, "allow") || strings.Contains(c, "enable") {
			t.Errorf("unexpected change to an inactive firewall: %q", c)
		}
	}
}

// A host with no firewall at all is normal, not an error.
func TestNoFirewallIsNotAFailure(t *testing.T) {
	r := &recorder{}
	close, opened := guard(r).Open(context.Background(), 9000)
	if opened {
		t.Error("nothing to open")
	}
	close() // must not panic
}

// If the rule cannot be added, serve anyway. The operator finds out from the
// page not loading; refusing to start would be a worse outcome than a port they
// can open themselves.
func TestFailureToOpenDoesNotStopSetup(t *testing.T) {
	r := &recorder{
		outputs: map[string]string{"ufw status": "Status: active"},
		fail:    map[string]bool{"ufw allow 9000/tcp comment Meshploy setup": true},
	}
	close, opened := guard(r, "ufw").Open(context.Background(), 9000)
	if opened {
		t.Error("the rule failed; opened should be false")
	}
	close() // must not attempt a delete for a rule that was never added
	if r.has("ufw delete allow 9000/tcp") {
		t.Error("should not delete a rule that was never added")
	}
}

// Closing runs at shutdown, when the serving context is already cancelled. The
// delete must still be attempted, or the port stays open.
func TestCloseWorksAfterTheContextIsCancelled(t *testing.T) {
	r := &recorder{outputs: map[string]string{"ufw status": "Status: active"}}
	ctx, cancel := context.WithCancel(context.Background())

	close, opened := guard(r, "ufw").Open(ctx, 9000)
	if !opened {
		t.Fatal("expected a rule")
	}
	cancel()
	close()

	if !r.has("ufw delete allow 9000/tcp") {
		t.Errorf("a cancelled context must not prevent closing the port: %v", r.calls)
	}
}

package setup

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
)

// Commander runs an external command. Injected so the firewall rules can be
// asserted in a test without altering the machine running it.
type Commander func(ctx context.Context, name string, args ...string) ([]byte, error)

func execCommander(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

// PortGuard opens the setup port for as long as the wizard is serving, and
// closes it afterwards.
//
// install.sh opens 80, 443, 53 and 3478 and nothing else, so on a host with an
// active firewall — which is most fresh cloud images — the setup page would be
// unreachable from the browser it was written for. Opening it here rather than
// in install.sh keeps the rule's lifetime tied to the process that needs it: a
// port left open after setup is a privileged installer left exposed.
type PortGuard struct {
	Run    Commander
	Lookup func(string) (string, error)
}

func NewPortGuard() PortGuard {
	return PortGuard{Run: execCommander, Lookup: exec.LookPath}
}

// Open adds a rule for the port and returns the function that removes it.
//
// Never fails the install: a host with no firewall, or one this does not know
// how to drive, simply gets no rule — and the operator finds out by the page
// not loading, which the printed URL already invites them to check. Refusing to
// serve because a firewall could not be adjusted would be worse.
func (g PortGuard) Open(ctx context.Context, port int) (close func(), opened bool) {
	p := strconv.Itoa(port)

	switch g.detect(ctx) {
	case "ufw":
		if _, err := g.Run(ctx, "ufw", "allow", p+"/tcp", "comment", "Meshploy setup"); err != nil {
			return func() {}, false
		}
		return func() {
			// Best effort: a failure to close is worth no noise at shutdown, and
			// the operator can see the rule with `ufw status`.
			_, _ = g.Run(context.WithoutCancel(ctx), "ufw", "delete", "allow", p+"/tcp")
		}, true

	case "firewalld":
		// Not --permanent: the rule should not survive a reboot, because setup
		// is over long before the next one.
		if _, err := g.Run(ctx, "firewall-cmd", "--add-port="+p+"/tcp"); err != nil {
			return func() {}, false
		}
		return func() {
			_, _ = g.Run(context.WithoutCancel(ctx), "firewall-cmd", "--remove-port="+p+"/tcp")
		}, true
	}

	return func() {}, false
}

// detect reports which firewall is active, if any. An installed but inactive
// firewall needs no rule and must not be started on the operator's behalf.
func (g PortGuard) detect(ctx context.Context) string {
	if _, err := g.Lookup("ufw"); err == nil {
		if out, err := g.Run(ctx, "ufw", "status"); err == nil &&
			strings.Contains(string(out), "Status: active") {
			return "ufw"
		}
	}
	if _, err := g.Lookup("firewall-cmd"); err == nil {
		if out, err := g.Run(ctx, "firewall-cmd", "--state"); err == nil &&
			strings.Contains(string(out), "running") {
			return "firewalld"
		}
	}
	return ""
}

package hostagent

import (
	"testing"
	"time"
)

// Captured from pnath-5-rt on 2026-09-17, where a TCP route on 10000 listened
// and nothing could reach it.
const pnathUFW = `Status: active
Logging: on (low)
Default: deny (incoming), allow (outgoing), allow (routed)
New profiles: skip

To                         Action      From
--                         ------      ----
22/tcp                     ALLOW IN    Anywhere                   # ssh
80/tcp                     ALLOW IN    Anywhere                   # caddy http
443/tcp                    ALLOW IN    Anywhere                   # caddy https
443/udp                    ALLOW IN    Anywhere                   # caddy http3
53/tcp                     ALLOW IN    Anywhere                   # coredns
53/udp                     ALLOW IN    Anywhere                   # coredns
41641/udp                  ALLOW IN    Anywhere                   # tailscale direct
Anywhere on tailscale0     ALLOW IN    Anywhere                   # mesh: k3s api, kubelet, flannel
Anywhere on br-b0271be08980 ALLOW IN    Anywhere                   # compose net to host
Anywhere on cni0           ALLOW IN    Anywhere                   # pods to host
22/tcp (v6)                ALLOW IN    Anywhere (v6)              # ssh
80/tcp (v6)                ALLOW IN    Anywhere (v6)              # caddy http
Anywhere (v6) on tailscale0 ALLOW IN    Anywhere (v6)              # mesh: k3s api, kubelet, flannel
`

func noResolve(string) string { return "" }

func fresh(fw Firewall) (*Agent, *Firewall, time.Time) {
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	fw.CheckedAt = now.Add(-30 * time.Second)
	return &Agent{HeartbeatAt: now.Add(-20 * time.Second)}, &fw, now
}

func TestParseUFWStatusFromAGateway(t *testing.T) {
	fw := ParseUFWStatus(pnathUFW, noResolve)
	if !fw.Active || fw.DefaultIncoming != "deny" {
		t.Fatalf("active=%v default=%q", fw.Active, fw.DefaultIncoming)
	}
	if len(fw.Rules) != 10 {
		t.Fatalf("got %d rules, want 10 (IPv6 duplicates folded): %+v", len(fw.Rules), fw.Rules)
	}
	if r := fw.Rules[8]; r.Interface != "br-b0271be08980" || r.Ports != "" || r.Action != "allow" {
		t.Errorf("interface rule with a long name read as %+v", r)
	}
	if r := fw.Rules[3]; r.Ports != "443" || r.Proto != "udp" {
		t.Errorf("443/udp read as %+v", r)
	}

	agent, report, now := fresh(fw)
	for port, want := range map[int]string{
		10000: PortBlocked, // the route that could not be reached
		22:    PortOpen,
		443:   PortOpen,
		41641: PortBlocked, // allowed for udp only
		6443:  PortBlocked, // allowed only on tailscale0, which is not the internet
	} {
		if got := Evaluate(agent, report, port, "tcp", now); got.State != want {
			t.Errorf("port %d: %s, want %s", port, got.State, want)
		}
	}
}

// The other gateway: ufw installed and off.
func TestInactiveUFWLeavesEveryPortOpen(t *testing.T) {
	fw := ParseUFWStatus("Status: inactive\n", noResolve)
	agent, report, now := fresh(fw)
	if got := Evaluate(agent, report, 10000, "tcp", now); got.State != PortOpen {
		t.Errorf("got %s, want open", got.State)
	}
}

func TestUFWRuleShapes(t *testing.T) {
	out := `Status: active
Default: deny (incoming), allow (outgoing), disabled (routed)

To                         Action      From
--                         ------      ----
5432/tcp                   ALLOW IN    203.0.113.0/24
5432/tcp                   ALLOW IN    10.0.0.0/8
60000:61000/tcp            ALLOW IN    Anywhere
80,443/tcp                 ALLOW IN    Anywhere
3306                       DENY IN     Anywhere
3306/tcp                   ALLOW IN    Anywhere
OpenSSH                    ALLOW IN    Anywhere
9000/tcp                   ALLOW OUT   Anywhere
`
	resolve := func(app string) string {
		if app == "OpenSSH" {
			return "22/tcp"
		}
		return ""
	}
	fw := ParseUFWStatus(out, resolve)
	agent, report, now := fresh(fw)
	cases := []struct {
		port  int
		state string
	}{
		{5432, PortRestricted},
		{60500, PortOpen},
		{443, PortOpen},
		{3306, PortBlocked}, // the deny comes first, and ufw stops at the first match
		{22, PortOpen},      // OpenSSH resolved
		{9000, PortBlocked}, // allowed outgoing only
	}
	for _, c := range cases {
		got := Evaluate(agent, report, c.port, "tcp", now)
		if got.State != c.state {
			t.Errorf("port %d: %+v, want %s", c.port, got, c.state)
		}
	}
	if got := Evaluate(agent, report, 5432, "tcp", now); len(got.Sources) != 2 || got.Sources[0] != "203.0.113.0/24" {
		t.Errorf("sources = %v", got.Sources)
	}
}

// A profile the agent could not resolve might cover any port, so a port no
// earlier rule settled cannot be called blocked, or open.
func TestUnresolvedProfileMakesLaterPortsUnknown(t *testing.T) {
	out := `Status: active
Default: deny (incoming), allow (outgoing), disabled (routed)

To                         Action      From
--                         ------      ----
22/tcp                     ALLOW IN    Anywhere
Nginx Full                 ALLOW IN    Anywhere
`
	agent, report, now := fresh(ParseUFWStatus(out, noResolve))
	if got := Evaluate(agent, report, 22, "tcp", now); got.State != PortOpen {
		t.Errorf("port 22, settled before the profile: %s", got.State)
	}
	if got := Evaluate(agent, report, 10000, "tcp", now); got.State != PortUnknown {
		t.Errorf("port 10000: %s, want unknown", got.State)
	}
}

func TestNoCurrentReportIsUnknown(t *testing.T) {
	fw := ParseUFWStatus(pnathUFW, noResolve)
	agent, report, now := fresh(fw)
	if got := Evaluate(nil, nil, 10000, "tcp", now); got.State != PortUnknown {
		t.Errorf("no agent: %s", got.State)
	}
	if got := Evaluate(agent, report, 10000, "tcp", now.Add(StaleAfter+time.Minute)); got.State != PortUnknown {
		t.Errorf("stale report: %s", got.State)
	}
	report.Error = "ufw: command failed"
	if got := Evaluate(agent, report, 10000, "tcp", now); got.State != PortUnknown {
		t.Errorf("failed check: %s", got.State)
	}
}

func TestParseFirewalldZone(t *testing.T) {
	out := `public (active)
  target: default
  icmp-block-inversion: no
  interfaces: eth0
  sources:
  services: dhcpv6-client ssh
  ports: 80/tcp 443/tcp 30000-32767/tcp
  protocols:
  forward: yes
  masquerade: no
  forward-ports:
  source-ports:
  icmp-blocks:
  rich rules:
`
	resolve := func(svc string) string {
		return map[string]string{"ssh": "22/tcp", "dhcpv6-client": "546/udp"}[svc]
	}
	fw := ParseFirewalldZone(out, resolve)
	agent, report, now := fresh(fw)
	for port, want := range map[int]string{22: PortOpen, 443: PortOpen, 31000: PortOpen, 10000: PortBlocked} {
		if got := Evaluate(agent, report, port, "tcp", now); got.State != want {
			t.Errorf("port %d: %s, want %s", port, got.State, want)
		}
	}

	withRich := ParseFirewalldZone(out+`	rule family="ipv4" source address="10.0.0.0/8" port port="10000" protocol="tcp" accept`+"\n", resolve)
	withRich.Unparsed = true
	agent, report, now = fresh(withRich)
	if got := Evaluate(agent, report, 10000, "tcp", now); got.State != PortUnknown {
		t.Errorf("port only a rich rule might allow: %s, want unknown", got.State)
	}
}

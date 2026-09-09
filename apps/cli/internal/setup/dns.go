// Package setup backs `meshploy setup serve` — the browser-driven install.
//
// It lives in the CLI rather than in a container because the CLI binary is
// already on the box before any container starts, runs on the host as root
// (which is what writing .env and driving compose needs), and is not subject to
// the trap that kills the container approach: a container cannot bind to the
// mesh IP during phase 1, because tailscale0 does not exist yet.
package setup

import (
	"context"
	"net"
	"time"
)

// Resolver is the DNS surface the checks need, narrowed so tests can supply
// answers instead of depending on the network and on whatever the operator's
// registrar has published this minute.
type Resolver interface {
	LookupNS(ctx context.Context, host string) ([]*net.NS, error)
	LookupHost(ctx context.Context, host string) ([]string, error)
}

// PublicResolver queries a public resolver rather than the system one.
//
// This matters: the gateway runs CoreDNS and is authoritative for its own zone
// in delegation mode, so asking the system resolver would cheerfully answer
// from the local zone file and report success for a delegation the rest of the
// internet cannot see. The question is always "what does the world see".
func PublicResolver(addr string) Resolver {
	if addr == "" {
		addr = "8.8.8.8:53"
	}
	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			d := net.Dialer{Timeout: 5 * time.Second}
			return d.DialContext(ctx, network, addr)
		},
	}
}

// HostStatus is one hostname's resolution, as seen from a public resolver.
type HostStatus struct {
	Host    string   `json:"host"`
	Addrs   []string `json:"addrs"`
	Matches bool     `json:"matches"` // resolves to the expected address
	Error   string   `json:"error,omitempty"`
}

// DomainStatus is everything the wizard shows on its verification step.
type DomainStatus struct {
	Domain string `json:"domain"`
	Mode   string `json:"mode"` // "delegation" or "ondemand"
	// PublicIP is what the records are expected to point at.
	PublicIP string `json:"public_ip"`

	// Delegated is meaningful only in delegation mode: whether an NS record for
	// the domain is visible publicly.
	Delegated   bool     `json:"delegated"`
	NameServers []string `json:"name_servers,omitempty"`

	Hosts []HostStatus `json:"hosts"`

	// Ready reports whether the wizard may move on. Deliberately not "every
	// check passed": a certificate is not required, and in on-demand mode none
	// may exist yet.
	Ready  bool   `json:"ready"`
	Reason string `json:"reason,omitempty"`
}

// hostnames are the names an install needs resolvable, whichever DNS mode is in
// use. The console is what the operator is about to be redirected to, and the
// API is what a joining worker and the browser both call.
func hostnames(domain string) []string {
	return []string{"console." + domain, "api." + domain}
}

// CheckDomain reports what the public internet currently sees for a domain.
//
// Never returns an error: every failure is a finding to display, not a reason
// to stop. The wizard exists precisely for the case where DNS is wrong.
func CheckDomain(ctx context.Context, r Resolver, domain, publicIP, mode string) DomainStatus {
	st := DomainStatus{Domain: domain, Mode: mode, PublicIP: publicIP}

	if domain == "" {
		st.Reason = "no domain set"
		return st
	}

	if mode == "delegation" {
		if ns, err := r.LookupNS(ctx, domain); err == nil && len(ns) > 0 {
			st.Delegated = true
			for _, n := range ns {
				st.NameServers = append(st.NameServers, n.Host)
			}
		}
	}

	for _, h := range hostnames(domain) {
		hs := HostStatus{Host: h}
		addrs, err := r.LookupHost(ctx, h)
		if err != nil {
			hs.Error = err.Error()
		}
		hs.Addrs = addrs
		for _, a := range addrs {
			if a == publicIP {
				hs.Matches = true
				break
			}
		}
		st.Hosts = append(st.Hosts, hs)
	}

	st.Ready, st.Reason = readiness(st)
	return st
}

// readiness decides whether setup may proceed.
//
// The bar is "the records point here", not "TLS works". In on-demand mode a
// certificate is issued per hostname on first request, so requiring one would
// block an install that is in fact correct — and would trap the operator on the
// step, which is the failure this whole feature exists to remove.
func readiness(st DomainStatus) (bool, string) {
	var unresolved []string
	for _, h := range st.Hosts {
		if !h.Matches {
			unresolved = append(unresolved, h.Host)
		}
	}
	if len(unresolved) == 0 {
		return true, ""
	}

	// In delegation mode a visible NS record is the real signal: the gateway's
	// own CoreDNS answers for the zone once traffic reaches it, so the A records
	// follow on their own and waiting for them only adds delay.
	if st.Mode == "delegation" && st.Delegated {
		return true, "NS delegation is visible; the gateway now answers for this zone"
	}

	if st.Mode == "delegation" && !st.Delegated {
		return false, "no NS record for " + st.Domain + " is visible yet"
	}
	return false, "not resolving to " + st.PublicIP + " yet: " + join(unresolved)
}

func join(s []string) string {
	out := ""
	for i, v := range s {
		if i > 0 {
			out += ", "
		}
		out += v
	}
	return out
}

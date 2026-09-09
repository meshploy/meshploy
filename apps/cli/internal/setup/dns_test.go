package setup

import (
	"context"
	"errors"
	"net"
	"testing"
)

// fakeResolver answers from a table so the checks can be exercised without
// depending on the network, or on what a registrar has published this minute.
type fakeResolver struct {
	ns    map[string][]string
	hosts map[string][]string
}

func (f fakeResolver) LookupNS(_ context.Context, host string) ([]*net.NS, error) {
	got, ok := f.ns[host]
	if !ok {
		return nil, errors.New("no NS records")
	}
	out := make([]*net.NS, len(got))
	for i, h := range got {
		out[i] = &net.NS{Host: h}
	}
	return out, nil
}

func (f fakeResolver) LookupHost(_ context.Context, host string) ([]string, error) {
	got, ok := f.hosts[host]
	if !ok {
		return nil, errors.New("no such host")
	}
	return got, nil
}

const ip = "203.0.113.10"

// The self-managed path: the operator adds A records themselves, so the check
// is simply whether the names point here.
func TestOnDemandReadyWhenRecordsPointHere(t *testing.T) {
	r := fakeResolver{hosts: map[string][]string{
		"console.example.com": {ip},
		"api.example.com":     {ip},
	}}
	st := CheckDomain(context.Background(), r, "example.com", ip, "ondemand")

	if !st.Ready {
		t.Fatalf("want ready, got not ready: %s", st.Reason)
	}
	for _, h := range st.Hosts {
		if !h.Matches {
			t.Errorf("%s should match %s, got %v", h.Host, ip, h.Addrs)
		}
	}
}

// Pointing somewhere else is the common mistake — an old server, or a
// proxied record. It must read as "not here yet", not as success.
func TestOnDemandNotReadyWhenPointingElsewhere(t *testing.T) {
	r := fakeResolver{hosts: map[string][]string{
		"console.example.com": {"198.51.100.7"},
		"api.example.com":     {"198.51.100.7"},
	}}
	st := CheckDomain(context.Background(), r, "example.com", ip, "ondemand")

	if st.Ready {
		t.Fatal("a domain pointing at another server must not read as ready")
	}
	if st.Reason == "" {
		t.Error("the operator needs to be told what is wrong")
	}
}

// In delegation mode a visible NS record is the signal that matters: once
// traffic reaches the gateway its own CoreDNS answers for the zone, so waiting
// for the A records to appear publicly only adds delay.
func TestDelegationReadyOnVisibleNSAlone(t *testing.T) {
	r := fakeResolver{
		ns:    map[string][]string{"example.com": {"ns.example.com."}},
		hosts: map[string][]string{}, // nothing resolves yet
	}
	st := CheckDomain(context.Background(), r, "example.com", ip, "delegation")

	if !st.Delegated {
		t.Fatal("the NS record should have been seen")
	}
	if !st.Ready {
		t.Fatalf("a visible delegation is enough to proceed, got: %s", st.Reason)
	}
	if len(st.NameServers) != 1 {
		t.Errorf("the nameservers should be shown, got %v", st.NameServers)
	}
}

func TestDelegationNotReadyWithoutNS(t *testing.T) {
	r := fakeResolver{hosts: map[string][]string{}}
	st := CheckDomain(context.Background(), r, "example.com", ip, "delegation")

	if st.Ready {
		t.Fatal("no delegation and nothing resolving must not read as ready")
	}
	if st.Delegated {
		t.Error("nothing published an NS record")
	}
}

// A resolver that errors is a finding to show, never a reason to stop: the
// wizard is the only screen that can fix a broken domain, so it must render.
func TestCheckNeverFailsHard(t *testing.T) {
	st := CheckDomain(context.Background(), fakeResolver{}, "example.com", ip, "ondemand")

	if len(st.Hosts) != 2 {
		t.Fatalf("every hostname must still be reported, got %d", len(st.Hosts))
	}
	for _, h := range st.Hosts {
		if h.Error == "" {
			t.Errorf("%s failed to resolve and should say so", h.Host)
		}
	}
	if st.Ready {
		t.Error("nothing resolved; this is not ready")
	}
}

// An empty domain is the state the wizard opens in. It must render, not panic.
func TestEmptyDomainIsReportedNotFatal(t *testing.T) {
	st := CheckDomain(context.Background(), fakeResolver{}, "", ip, "ondemand")
	if st.Ready || st.Reason == "" {
		t.Fatalf("an unset domain should report a reason, got %+v", st)
	}
}

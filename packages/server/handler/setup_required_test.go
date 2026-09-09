package handler

import "testing"

// The gate must fire on a half-configured gateway and stay silent on a
// developer's machine. DOMAIN is optional, missing from .env.example and unset
// in local development, so gating on it alone would lock every developer out of
// the console — breaking a working flow to protect one that is not.
func TestSetupRequired(t *testing.T) {
	cases := []struct {
		name                                    string
		domain, publicIP, gatewayIP, gwHostname string
		want                                    bool
	}{
		{
			name: "developer machine — no gateway seeding at all",
			want: false,
		},
		{
			name:   "developer machine with a domain set",
			domain: "localhost", want: false,
		},
		{
			name:     "gateway installed but abandoned before the domain",
			publicIP: "203.0.113.10", want: true,
		},
		{
			name:      "gateway known only by its mesh IP",
			gatewayIP: "100.64.0.1", want: true,
		},
		{
			name:       "gateway known only by hostname",
			gwHostname: "gw-1", want: true,
		},
		{
			name:   "fully configured gateway",
			domain: "example.com", publicIP: "203.0.113.10", gatewayIP: "100.64.0.1",
			want: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := setupRequired(c.domain, c.publicIP, c.gatewayIP, c.gwHostname); got != c.want {
				t.Errorf("setupRequired(%q,%q,%q,%q) = %v, want %v",
					c.domain, c.publicIP, c.gatewayIP, c.gwHostname, got, c.want)
			}
		})
	}
}

// The console reads both fields off this one public endpoint, so both must
// reach the wire. AuthStatusOutput is a plain struct, but the config-file DTO
// proved that assumption is worth checking rather than trusting.
func TestAuthStatusSchemaCarriesBothFields(t *testing.T) {
	props := schemaProperties(t, AuthStatusOutput{}.Body, "AuthStatus")
	for _, want := range []string{"registration_open", "setup_required"} {
		if _, ok := props[want]; !ok {
			t.Errorf("response schema is missing %q", want)
		}
	}
}

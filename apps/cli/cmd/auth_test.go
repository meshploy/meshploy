package cmd

import (
	"reflect"
	"testing"
)

// What a person types for a server is understood as the API address it
// means, and the console to approve at when it names one.
func TestATypedServerIsUnderstood(t *testing.T) {
	for _, c := range []struct {
		given string
		want  []serverCandidate
	}{
		{"example.com", []serverCandidate{{api: "https://api.example.com"}}},
		{"api.example.com", []serverCandidate{{api: "https://api.example.com"}}},
		{"https://api.example.com/some/path", []serverCandidate{{api: "https://api.example.com"}}},
		{"console.example.com", []serverCandidate{{api: "https://api.example.com"}, {api: "https://api.console.example.com"}}},
		{"https://apps.example.com/apps", []serverCandidate{{api: "https://api.example.com", door: "apps"}, {api: "https://api.apps.example.com"}}},
		{"deploy.example.co.uk", []serverCandidate{{api: "https://api.example.co.uk", door: "deploy"}, {api: "https://api.deploy.example.co.uk"}}},
		{"http://localhost:4000", []serverCandidate{{api: "http://localhost:4000"}}},
		{"http://100.64.0.1:4000", []serverCandidate{{api: "http://100.64.0.1:4000"}}},
		{" Example.COM ", []serverCandidate{{api: "https://api.example.com"}}},
	} {
		if got := serverCandidates(c.given); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%q: %+v, want %+v", c.given, got, c.want)
		}
	}
}

// A server is named by its domain when it is the usual api.<domain>.
func TestAServerIsNamedByItsDomain(t *testing.T) {
	for api, want := range map[string]string{
		"https://api.example.com":   "example.com",
		"http://100.64.0.1:4000":    "100.64.0.1:4000",
		"https://deploy.example.io": "deploy.example.io",
	} {
		if got := displayHost(api); got != want {
			t.Errorf("%s: %q, want %q", api, got, want)
		}
	}
}

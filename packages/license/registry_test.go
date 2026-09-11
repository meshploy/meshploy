package license

import "testing"

func TestValidRegistryScope(t *testing.T) {
	valid := []string{
		DefaultRegistryScope,
		"ghcr.io/meshploy/api-ee-acme",
		"ghcr.io/meshploy/api-ee.v2",
	}
	for _, s := range valid {
		if err := ValidRegistryScope(s); err != nil {
			t.Errorf("%q should be valid: %v", s, err)
		}
	}

	invalid := []string{
		"",                                     // nothing to pull
		"ee-acme",                              // the old README example: pulls docker.io/library/ee-acme
		"ghcr.io/meshploy/api-ee:latest",       // the tag follows the server's channel
		"ghcr.io/meshploy/api-ee@sha256:abc",   // nor a digest
		"ghcr.io/someone-else/api-ee",          // another owner's image
		"docker.io/meshploy/api-ee",            // another registry
		"ghcr.io/meshploy/team/api-ee",         // nested path
		"ghcr.io/meshploy/API-EE",              // not a valid repository name
		"ghcr.io/meshploy/api-ee\nrm -rf /",    // anything smuggled after it
		" ghcr.io/meshploy/api-ee",             // untrimmed
		"ghcr.io/meshploy/../api-ee",           // traversal
		"https://ghcr.io/meshploy/api-ee",      // a URL, not an image
		"ghcr.io/meshploy/-api-ee",             // repository names start alphanumeric
		"ghcr.io/meshploy/api-ee-",             // or end so
		"ghcr.io/meshploy/api--ee",             // no doubled separator
		"ghcr.io.evil.example/meshploy/api-ee", // a look-alike host
	}
	for _, s := range invalid {
		if err := ValidRegistryScope(s); err == nil {
			t.Errorf("%q should be rejected", s)
		}
	}
}

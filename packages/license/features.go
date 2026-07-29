package license

import "sort"

// Feature flags a license may grant.
//
// These live in the public package because four separate places need to agree
// on the exact strings: the issuer that writes them into a token
// (meshploy-admin), the verifier that reads them (this repository), the
// Enterprise code that gates on them, and Cloud, which will derive the same
// names from a plan rather than a token. Only this package is imported by all
// of them — the Enterprise repository cannot hold the list, because the public
// repository is forbidden from importing it.
//
// Publishing the names costs nothing. They are identifiers, not implementations;
// what stays private is the code behind each one.
const (
	FeatureSSO         = "sso"
	FeatureAuditLog    = "audit-log"
	FeatureMultiTenant = "multi-tenancy"
)

// knownFeatures is the set every recognised flag belongs to.
var knownFeatures = map[string]bool{
	FeatureSSO:         true,
	FeatureAuditLog:    true,
	FeatureMultiTenant: true,
}

// KnownFeatures returns every recognised flag, sorted, for help text and error
// messages.
func KnownFeatures() []string {
	out := make([]string, 0, len(knownFeatures))
	for f := range knownFeatures {
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}

// IsKnownFeature reports whether name is a flag this build recognises.
func IsKnownFeature(name string) bool { return knownFeatures[name] }

// UnknownFeatures returns the entries of names this build does not recognise,
// in the order given.
//
// Intended for the ISSUING side only. A typo there — "ss0" for "sso" — produces
// a token that signs, verifies, and activates cleanly while granting nothing,
// because the gate looks for a string that is not in the claim set. Nothing
// fails; the customer simply does not get what they paid for, and the first
// signal is a support request.
//
// Verification deliberately does not use this. An install must accept a license
// naming a feature it has never heard of: tokens outlive the builds that read
// them, and a customer on an older release should keep working after buying
// something newer rather than having their license rejected outright.
func UnknownFeatures(names []string) []string {
	var out []string
	for _, n := range names {
		if n != "" && !IsKnownFeature(n) {
			out = append(out, n)
		}
	}
	return out
}

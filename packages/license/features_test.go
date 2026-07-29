package license

import (
	"slices"
	"testing"
	"time"
)

func TestUnknownFeaturesCatchesATypo(t *testing.T) {
	// The failure this exists to prevent: "ss0" signs, verifies and activates
	// exactly like "sso", and grants nothing.
	got := UnknownFeatures([]string{FeatureSSO, "ss0", FeatureAuditLog})
	if !slices.Equal(got, []string{"ss0"}) {
		t.Fatalf("want only the typo reported, got %v", got)
	}
}

func TestUnknownFeaturesAcceptsAValidSet(t *testing.T) {
	if got := UnknownFeatures(KnownFeatures()); got != nil {
		t.Fatalf("every known feature must validate, got %v", got)
	}
}

// Empty entries come from splitting an empty or trailing-comma flag value and
// are not typos.
func TestUnknownFeaturesIgnoresEmptyEntries(t *testing.T) {
	if got := UnknownFeatures([]string{"", FeatureSSO, ""}); got != nil {
		t.Fatalf("empty entries should be ignored, got %v", got)
	}
}

// The load-bearing asymmetry: an install must honour a license naming features
// it has never heard of. Tokens outlive the builds that read them, so a
// customer on an older release must keep working after buying something newer.
func TestVerificationAcceptsUnknownFeatures(t *testing.T) {
	l := License{
		Features:  []string{FeatureSSO, "a-feature-from-the-future"},
		ExpiresAt: time.Now().Add(time.Hour),
	}

	if !l.HasFeature(FeatureSSO) {
		t.Fatal("a known feature alongside an unknown one must still be granted")
	}
	if l.HasFeature("never-granted") {
		t.Fatal("HasFeature must not report a feature the license does not name")
	}
}

// The constants are a wire contract: a token minted last year names them by
// value, so renaming one silently stops honouring every license already issued.
func TestFeatureValuesAreStable(t *testing.T) {
	for name, want := range map[string]string{
		"FeatureSSO":         "sso",
		"FeatureAuditLog":    "audit-log",
		"FeatureMultiTenant": "multi-tenancy",
	} {
		var got string
		switch name {
		case "FeatureSSO":
			got = FeatureSSO
		case "FeatureAuditLog":
			got = FeatureAuditLog
		case "FeatureMultiTenant":
			got = FeatureMultiTenant
		}
		if got != want {
			t.Fatalf("%s is %q, but licenses already issued name it %q — "+
				"changing it stops honouring them", name, got, want)
		}
	}
}

package service

import (
	"strings"
	"testing"
)

// A stack's volumes are named "<stack>-<volume>", and resolveNamedVolumes looks
// one up by (project, name) — so two stacks called "zot" made the second adopt
// the first's PVC, with both writing to one disk and nothing reporting it.
// Numbering the stack keeps the volume names apart.
func TestNextFreeNameNumbersFromTheSecond(t *testing.T) {
	existing := map[string]bool{}
	taken := func(s string) bool { return existing[s] }

	for _, want := range []string{"zot", "zot-2", "zot-3"} {
		got := nextFreeName("zot", taken)
		if got != want {
			t.Fatalf("want %q, got %q", want, got)
		}
		existing[got] = true
	}
}

// Deleting the middle instance frees its name, and reusing it is the right
// call: these are display names a person reads, not identifiers that outlive
// the resource. The Kubernetes names that must never be reused are the service
// slugs, which carry their own random suffix.
func TestNextFreeNameReusesAFreedNumber(t *testing.T) {
	existing := map[string]bool{"zot": true, "zot-3": true}
	got := nextFreeName("zot", func(s string) bool { return existing[s] })
	if got != "zot-2" {
		t.Fatalf("want the freed zot-2, got %q", got)
	}
}

// A project that somehow holds a hundred instances must still deploy rather
// than loop or return a name already in use.
func TestNextFreeNameGivesUpToSomethingFree(t *testing.T) {
	got := nextFreeName("zot", func(string) bool { return true })
	if !strings.HasPrefix(got, "zot-") || got == "zot" {
		t.Fatalf("want a suffixed fallback, got %q", got)
	}
}

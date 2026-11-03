package k8s

import "testing"

func TestCountDeletedReadsTheScriptsCount(t *testing.T) {
	if got := countDeleted("some warning\nremoved 43\n"); got != 43 {
		t.Errorf("countDeleted = %d, want 43", got)
	}
	for _, out := range []string{"", "removed 0\n", "removed -2\n", "Deleted: x\n"} {
		if got := countDeleted(out); got != 0 {
			t.Errorf("%q = %d, want 0", out, got)
		}
	}
	if got := lastLine("a\nfailed to connect\n"); got != "failed to connect" {
		t.Errorf("lastLine = %q", got)
	}
}

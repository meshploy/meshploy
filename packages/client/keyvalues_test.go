package client_test

import (
	"testing"

	"github.com/meshploy/packages/client"
)

func TestParseKeyValues(t *testing.T) {
	got, err := client.ParseKeyValues("# a comment\n\nNEO4J_PASSWORD=\"s3cr3t\"\nKEY=a=b\nPEM=line1\\nline2\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := map[string]string{
		"NEO4J_PASSWORD": "s3cr3t",
		"KEY":            "a=b",
		"PEM":            "line1\nline2",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
	if len(got) != len(want) {
		t.Errorf("want %d values, got %d: %v", len(want), len(got), got)
	}
	if _, err := client.ParseKeyValues("NOT_A_PAIR"); err == nil {
		t.Error("want an error for a line with no =")
	}
}

package cmd

import (
	"testing"
)

func TestIsUUID(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{"550e8400-e29b-41d4-a716-446655440000", true},  // valid v4
		{"00000000-0000-0000-0000-000000000000", true},  // all zeros
		{"", false},                                      // empty
		{"not-a-uuid", false},                            // too short
		{"550e8400e29b41d4a716446655440000", false},      // no hyphens
		{"550e8400-e29b-41d4-a716-44665544000Z", true},  // 36 chars with dashes in right spots — heuristic passes
		{"my-project-slug", false},                       // typical slug
		{"proj-abc", false},                              // short slug
	}
	for _, tc := range cases {
		got := isUUID(tc.input)
		if got != tc.want {
			t.Errorf("isUUID(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

func TestMergeEnvVars(t *testing.T) {
	cases := []struct {
		name     string
		existing string
		pairs    []string
		want     string
	}{
		{
			name:     "empty existing, add new keys",
			existing: "",
			pairs:    []string{"A=1", "B=2"},
			want:     "A=1\nB=2\n",
		},
		{
			name:     "overwrite existing key",
			existing: "A=old\nB=keep\n",
			pairs:    []string{"A=new"},
			want:     "A=new\nB=keep\n",
		},
		{
			name:     "append new key, preserve existing order",
			existing: "A=1\nB=2\n",
			pairs:    []string{"C=3"},
			want:     "A=1\nB=2\nC=3\n",
		},
		{
			name:     "overwrite and append together",
			existing: "A=1\nB=2\n",
			pairs:    []string{"B=99", "C=3"},
			want:     "A=1\nB=99\nC=3\n",
		},
		{
			name:     "blank lines and comments stripped from existing",
			existing: "# comment\n\nA=1\n",
			pairs:    []string{"B=2"},
			want:     "A=1\nB=2\n",
		},
		{
			name:     "windows CRLF in existing block",
			existing: "A=1\r\nB=2\r\n",
			pairs:    []string{"C=3"},
			want:     "A=1\nB=2\nC=3\n",
		},
		{
			name:     "value containing equals sign",
			existing: "",
			pairs:    []string{"DSN=postgres://user:pass@host/db?sslmode=disable"},
			want:     "DSN=postgres://user:pass@host/db?sslmode=disable\n",
		},
		{
			name:     "empty pairs — existing returned unchanged",
			existing: "A=1\n",
			pairs:    []string{},
			want:     "A=1\n",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := mergeEnvVars(tc.existing, tc.pairs)
			if got != tc.want {
				t.Errorf("\nwant: %q\n got: %q", tc.want, got)
			}
		})
	}
}

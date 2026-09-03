package templates

import (
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// A derived generator has to see the value it hashes, whichever order the two
// variables are declared in -- the second pass exists precisely so a template
// author does not have to think about ordering.
func TestResolveBcryptHashesReferencedVariable(t *testing.T) {
	for _, order := range []string{"hash-first", "hash-last"} {
		t.Run(order, func(t *testing.T) {
			pw := Variable{Key: "ZOT_PASSWORD", Generate: genPassword}
			hash := Variable{Key: "ZOT_HTPASSWD", Generate: "bcrypt(ZOT_PASSWORD)"}

			m := &Manifest{ID: "zot", Variables: []Variable{pw, hash}}
			if order == "hash-first" {
				m.Variables = []Variable{hash, pw}
			}

			vars, _, err := Resolve(m, nil, "example.com")
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if vars["ZOT_PASSWORD"] == "" {
				t.Fatal("password was not generated")
			}
			if !strings.HasPrefix(vars["ZOT_HTPASSWD"], "$2a$") {
				t.Fatalf("want a bcrypt hash, got %q", vars["ZOT_HTPASSWD"])
			}
			if err := bcrypt.CompareHashAndPassword(
				[]byte(vars["ZOT_HTPASSWD"]), []byte(vars["ZOT_PASSWORD"])); err != nil {
				t.Fatalf("hash does not verify against the password: %v", err)
			}
		})
	}
}

// A reference that cannot be satisfied must stop the template loading, not
// surface as an empty hash after half a stack has been created.
func TestParseManifestRejectsBadBcryptRefs(t *testing.T) {
	tests := map[string]string{
		"missing": "variables:\n  - key: H\n    generate: bcrypt(NOPE)\n",
		"self":    "variables:\n  - key: H\n    generate: bcrypt(H)\n",
		"chained": "variables:\n  - key: P\n    generate: password\n  - key: H\n    generate: bcrypt(P)\n  - key: H2\n    generate: bcrypt(H)\n",
	}
	for name, vars := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseManifest([]byte("id: t\n" + vars)); err == nil {
				t.Fatal("want an error, got none")
			}
		})
	}
}

// An empty password would hash fine and lock nobody out of anything, so the
// hash must never be produced from a value that was never generated.
func TestResolveBcryptOfPromptedValue(t *testing.T) {
	m := &Manifest{ID: "t", Variables: []Variable{
		{Key: "PW", Prompt: "Password", Required: true},
		{Key: "H", Generate: "bcrypt(PW)"},
	}}
	vars, _, err := Resolve(m, map[string]string{"PW": "hunter2"}, "example.com")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(vars["H"]), []byte("hunter2")); err != nil {
		t.Fatalf("prompted value did not hash correctly: %v", err)
	}
}

// bcrypt("") returns a valid hash that an empty password opens, so a derived
// generator over a blank optional prompt would produce a service anyone can log
// into. That has to fail loudly at resolve time.
func TestResolveBcryptRefusesEmptyValue(t *testing.T) {
	m := &Manifest{ID: "t", Variables: []Variable{
		{Key: "PW", Prompt: "Password"}, // optional -- may legitimately be blank
		{Key: "H", Generate: "bcrypt(PW)"},
	}}
	_, _, err := Resolve(m, map[string]string{"PW": ""}, "example.com")
	if err == nil {
		t.Fatal("want an error hashing an empty value, got none")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Fatalf("error should say what went wrong, got: %v", err)
	}
}

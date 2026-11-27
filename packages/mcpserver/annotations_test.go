package mcpserver

import (
	"strings"
	"testing"

	"github.com/meshploy/packages/client"
)

// Every CE tool is in the annotations table, and the table names no tool that
// is not there: a new tool cannot ship claiming the defaults (destructive,
// open world), and a removed one leaves no stale entry.
func TestEveryToolIsAnnotated(t *testing.T) {
	ms := New(client.New("http://127.0.0.1:1", ""), "org")
	tools := ms.ListTools()
	for name, st := range tools {
		a := st.Tool.Annotations
		if _, ok := toolKinds[name]; !ok {
			t.Errorf("%s is not in toolKinds", name)
			continue
		}
		if a.Title == "" || strings.Contains(a.Title, "_") || strings.ContainsRune(a.Title, '—') {
			t.Errorf("%s: title %q", name, a.Title)
		}
		if a.ReadOnlyHint == nil || a.DestructiveHint == nil || a.IdempotentHint == nil || a.OpenWorldHint == nil {
			t.Errorf("%s: a hint is unset", name)
		}
	}
	for _, m := range []map[string]bool{openWorld, idempotent} {
		for name := range m {
			if _, ok := tools[name]; !ok {
				t.Errorf("%s is annotated but is no tool", name)
			}
		}
	}
	for name := range toolKinds {
		if _, ok := tools[name]; !ok {
			t.Errorf("%s is in toolKinds but is no tool", name)
		}
	}
}

// The hints follow the names where the names are plain: a list or get reads
// only, a delete, restore or remove can lose something, and a tool whose
// description warns it is destructive is marked so.
func TestHintsMatchWhatToolsSay(t *testing.T) {
	ms := New(client.New("http://127.0.0.1:1", ""), "org")
	for name, st := range ms.ListTools() {
		a := st.Tool.Annotations
		verb := strings.SplitN(name, "_", 2)[0]
		switch verb {
		case "list", "get":
			if !*a.ReadOnlyHint {
				t.Errorf("%s reads but is not read-only", name)
			}
		case "delete", "restore", "remove", "revoke":
			if !*a.DestructiveHint {
				t.Errorf("%s is not marked destructive", name)
			}
		}
		if strings.HasPrefix(st.Tool.Description, "DESTRUCTIVE") && !*a.DestructiveHint {
			t.Errorf("%s says it is destructive and is not marked so", name)
		}
		if *a.ReadOnlyHint && *a.DestructiveHint {
			t.Errorf("%s is both read-only and destructive", name)
		}
	}
}

func TestToolTitle(t *testing.T) {
	for name, want := range map[string]string{
		"get_env_vars":         "Get environment variables",
		"check_dns_provider":   "Check DNS provider",
		"upsert_system_backup": "Set up system backup",
		"db_query":             "Run a database query",
	} {
		if got := toolTitle(name); got != want {
			t.Errorf("toolTitle(%s) = %q, want %q", name, got, want)
		}
	}
}

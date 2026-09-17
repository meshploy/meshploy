package hostagent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const goodID = "3f2b8c1e-9a4d-4e6f-8b2a-1c3d5e7f9a0b"

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The API can write anything into the inbox, so only a request with exactly
// the expected shape is accepted.
func TestReadRequestFileTrustsOnlyTheShape(t *testing.T) {
	dir := t.TempDir()
	good := Request{ID: goodID, Type: RequestMigratePlan, RequestedBy: "u1", RequestedAt: time.Now()}
	name := RequestFileName(good)
	writeFile(t, filepath.Join(dir, name), `{"id":"`+goodID+`","type":"migrate.plan","requested_by":"u1","requested_at":"2026-09-17T12:00:00Z"}`)
	if req, err := ReadRequestFile(filepath.Join(dir, name)); err != nil || req.Type != RequestMigratePlan {
		t.Fatalf("good request: %+v, %v", req, err)
	}

	cases := map[string]struct{ name, body string }{
		"unknown type":  {"migrate.apply-" + goodID + ".json", `{"id":"` + goodID + `","type":"migrate.apply"}`},
		"name mismatch": {"migrate.detect-" + goodID + ".json", `{"id":"` + goodID + `","type":"migrate.plan"}`},
		"extra field":   {name, `{"id":"` + goodID + `","type":"migrate.plan","command":"rm -rf /"}`},
		"not json":      {name, `nope`},
		"bad name":      {"../../etc/passwd", `{}`},
		"too large":     {name, `{"id":"` + goodID + `","type":"migrate.plan","requested_by":"` + strings.Repeat("x", MaxRequestBytes) + `"}`},
	}
	for label, c := range cases {
		t.Run(label, func(t *testing.T) {
			d := t.TempDir()
			path := filepath.Join(d, filepath.Base(c.name))
			writeFile(t, path, c.body)
			if _, err := ReadRequestFile(filepath.Join(d, c.name)); err == nil {
				t.Errorf("accepted %s", label)
			}
		})
	}

	t.Run("symlink", func(t *testing.T) {
		d := t.TempDir()
		target := filepath.Join(d, "target.json")
		writeFile(t, target, `{"id":"`+goodID+`","type":"migrate.plan"}`)
		link := filepath.Join(d, name)
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadRequestFile(link); err == nil {
			t.Error("followed a symlink")
		}
	})
}

func TestReadResultPassesJSONThrough(t *testing.T) {
	dir := t.TempDir()
	if raw, at, err := ReadResult(dir, PlanFile); raw != nil || at != nil || err != nil {
		t.Fatalf("no result: %s %v %v", raw, at, err)
	}
	if err := os.MkdirAll(MigrateDir(dir), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(MigrateDir(dir), PlanFile), `{"summary":{"moves":3}}`)
	raw, at, err := ReadResult(dir, PlanFile)
	if err != nil || at == nil || !strings.Contains(string(raw), `"moves":3`) {
		t.Errorf("got %s %v %v", raw, at, err)
	}
	writeFile(t, filepath.Join(MigrateDir(dir), DetectFile), `{broken`)
	if _, _, err := ReadResult(dir, DetectFile); err == nil {
		t.Error("accepted invalid JSON")
	}
}

package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

// A side-loaded agent replaces the installed one whole, executable.
func TestTheClusterAgentIsInstalledFromAFile(t *testing.T) {
	dir := t.TempDir()
	src, dest := filepath.Join(dir, "agent"), filepath.Join(dir, "bin", "meshploy-cluster-agent")
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(src, []byte("new"), 0o600)
	os.WriteFile(dest, []byte("old"), 0o755)
	if err := copyExecutable(src, dest); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(dest)
	info, _ := os.Stat(dest)
	if string(got) != "new" || info.Mode().Perm() != 0o755 {
		t.Fatalf("installed %q, %v", got, info.Mode())
	}
	if left, _ := filepath.Glob(filepath.Join(filepath.Dir(dest), ".meshploy-install-*")); len(left) != 0 {
		t.Fatalf("left behind %v", left)
	}
	if got := clusterAgentAsset("arm64"); got != "meshploy-cluster-agent-linux-arm64" {
		t.Fatalf("asset %s", got)
	}
}

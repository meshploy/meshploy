package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/meshploy/apps/cli/internal/config"
)

// chdir changes the working directory and restores it on cleanup.
func chdir(t *testing.T, dir string) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	t.Cleanup(func() { os.Chdir(orig) })
}

func TestSaveAndLoadProjectLink(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	if err := config.SaveProjectLink("proj-abc"); err != nil {
		t.Fatalf("SaveProjectLink: %v", err)
	}

	link, err := config.LoadProjectLink()
	if err != nil {
		t.Fatalf("LoadProjectLink: %v", err)
	}
	if link == nil {
		t.Fatal("expected non-nil link")
	}
	if link.ProjectID != "proj-abc" {
		t.Errorf("want proj-abc, got %q", link.ProjectID)
	}
}

func TestLoadProjectLink_WalksUp(t *testing.T) {
	// Layout: root/ (has .meshploy) → root/sub/ (cwd)
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	if err := os.Mkdir(sub, 0755); err != nil {
		t.Fatalf("mkdir sub: %v", err)
	}

	// Write .meshploy in root
	chdir(t, root)
	if err := config.SaveProjectLink("proj-root"); err != nil {
		t.Fatalf("SaveProjectLink: %v", err)
	}

	// Load from sub — should walk up and find it
	chdir(t, sub)
	link, err := config.LoadProjectLink()
	if err != nil {
		t.Fatalf("LoadProjectLink: %v", err)
	}
	if link == nil {
		t.Fatal("expected link found in parent directory")
	}
	if link.ProjectID != "proj-root" {
		t.Errorf("want proj-root, got %q", link.ProjectID)
	}
}

func TestLoadProjectLink_NotFound(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	link, err := config.LoadProjectLink()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if link != nil {
		t.Errorf("expected nil link, got %+v", link)
	}
}

func TestRemoveProjectLink(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	if err := config.SaveProjectLink("proj-tmp"); err != nil {
		t.Fatalf("SaveProjectLink: %v", err)
	}
	if err := config.RemoveProjectLink(); err != nil {
		t.Fatalf("RemoveProjectLink: %v", err)
	}

	// File should be gone
	link, err := config.LoadProjectLink()
	if err != nil {
		t.Fatalf("LoadProjectLink after remove: %v", err)
	}
	if link != nil {
		t.Errorf("expected nil after remove, got %+v", link)
	}
}

func TestRemoveProjectLink_NotExist(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	// Should be a no-op
	if err := config.RemoveProjectLink(); err != nil {
		t.Errorf("RemoveProjectLink on non-existent file should not error, got: %v", err)
	}
}

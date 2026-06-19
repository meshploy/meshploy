package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/meshploy/apps/cli/internal/config"
)

// pointHomeAt redirects $HOME to a temp dir and restores it on cleanup.
func pointHomeAt(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	orig := os.Getenv("HOME")
	os.Setenv("HOME", dir)
	t.Cleanup(func() { os.Setenv("HOME", orig) })
	// UserHomeDir on Windows uses USERPROFILE, not HOME
	origUP := os.Getenv("USERPROFILE")
	os.Setenv("USERPROFILE", dir)
	t.Cleanup(func() { os.Setenv("USERPROFILE", origUP) })
	return dir
}

func TestLoad_NotExist(t *testing.T) {
	pointHomeAt(t)
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	// Empty config when file doesn't exist yet
	if cfg.Token != "" || cfg.APIURL != "" || cfg.OrgID != "" {
		t.Errorf("expected zero config, got %+v", cfg)
	}
}

func TestSaveAndLoad(t *testing.T) {
	pointHomeAt(t)

	want := &config.Config{
		APIURL: "https://meshploy.example.com",
		Token:  "jwt-abc",
		OrgID:  "org-123",
	}

	if err := config.Save(want); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.APIURL != want.APIURL {
		t.Errorf("APIURL: want %q, got %q", want.APIURL, got.APIURL)
	}
	if got.Token != want.Token {
		t.Errorf("Token: want %q, got %q", want.Token, got.Token)
	}
	if got.OrgID != want.OrgID {
		t.Errorf("OrgID: want %q, got %q", want.OrgID, got.OrgID)
	}
}

func TestSave_CreatesDirectory(t *testing.T) {
	home := pointHomeAt(t)
	meshployDir := filepath.Join(home, ".meshploy")

	// ensure the dir doesn't exist yet
	if _, err := os.Stat(meshployDir); !os.IsNotExist(err) {
		t.Skip("directory already exists, skipping")
	}

	if err := config.Save(&config.Config{Token: "tok"}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if _, err := os.Stat(meshployDir); err != nil {
		t.Errorf("expected directory to be created: %v", err)
	}
}

func TestClear(t *testing.T) {
	pointHomeAt(t)

	// Save then clear
	if err := config.Save(&config.Config{Token: "tok"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := config.Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}

	// Load should now return an empty config (not an error)
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load after Clear: %v", err)
	}
	if cfg.Token != "" {
		t.Errorf("expected empty token after Clear, got %q", cfg.Token)
	}
}

func TestClear_NotExist(t *testing.T) {
	pointHomeAt(t)
	// Clearing when no file exists should be a no-op
	if err := config.Clear(); err != nil {
		t.Errorf("Clear on non-existent file should not error, got: %v", err)
	}
}

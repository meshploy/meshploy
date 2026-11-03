package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// imageGCFixture points the drop-in at a temporary k3s directory, fixes the
// release, and records restarts instead of performing them.
func imageGCFixture(t *testing.T, release, env string) (path string, restarts *[]string) {
	t.Helper()
	origDropin, origRelease, origRestart, origDir := imageGCDropin, k3sRelease, systemctlRestart, meshployInstDir
	t.Cleanup(func() {
		imageGCDropin, k3sRelease, systemctlRestart, meshployInstDir = origDropin, origRelease, origRestart, origDir
	})
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "rancher", "k3s"), 0o755); err != nil {
		t.Fatal(err)
	}
	imageGCDropin = filepath.Join(root, "rancher", "k3s", "config.yaml.d", "30-image-cleanup.yaml")
	k3sRelease = func() string { return release }
	seen := []string{}
	systemctlRestart = func(unit string) error { seen = append(seen, unit); return nil }
	meshployInstDir = t.TempDir()
	if err := os.WriteFile(filepath.Join(meshployInstDir, ".env"), []byte(env), 0o600); err != nil {
		t.Fatal(err)
	}
	return imageGCDropin, &seen
}

// An upgrade turns the daily cleanup on, once: the next upgrade finds the
// file as it should be and restarts nothing.
func TestCleanImagesDailyWritesOnce(t *testing.T) {
	dropin, restarts := imageGCFixture(t, "v1.36.5+k3s1", "DOMAIN=example.com\n")
	for i := 0; i < 2; i++ {
		if err := cleanImagesDaily(); err != nil {
			t.Fatal(err)
		}
	}
	body, err := os.ReadFile(dropin)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"kubelet-arg+:", "image-maximum-gc-age=24h", "image-gc-high-threshold=80"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("drop-in lacks %q:\n%s", want, body)
		}
	}
	if len(*restarts) != 1 {
		t.Errorf("want one k3s restart, got %v", *restarts)
	}
}

// The age is the operator's to set, and off takes the drop-in away again.
func TestCleanImagesDailyFollowsTheSetting(t *testing.T) {
	dropin, restarts := imageGCFixture(t, "v1.36.5+k3s1", "IMAGE_CLEANUP_AFTER=72h\n")
	if err := cleanImagesDaily(); err != nil {
		t.Fatal(err)
	}
	if body, _ := os.ReadFile(dropin); !strings.Contains(string(body), "image-maximum-gc-age=72h") {
		t.Errorf("drop-in does not carry the operator's age:\n%s", body)
	}
	if err := setEnvVar("IMAGE_CLEANUP_AFTER", "off"); err != nil {
		t.Fatal(err)
	}
	if err := cleanImagesDaily(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dropin); !os.IsNotExist(err) {
		t.Errorf("off left the drop-in: %v", err)
	}
	if len(*restarts) != 2 {
		t.Errorf("want a restart for each change, got %v", *restarts)
	}
}

// Kubernetes before 1.30 refuses the age without its feature gate, which
// would stop kubelet starting; such a node gets the thresholds alone.
func TestImageGCAgeOnlyFromKubernetes130(t *testing.T) {
	if strings.Contains(imageGCWant("24h", "v1.29.4+k3s1"), "image-maximum-gc-age") {
		t.Error("1.29 was given the age")
	}
	for _, r := range []string{"v1.30.0+k3s1", "v1.36.5+k3s1", ""} {
		if !strings.Contains(imageGCWant("24h", r), "image-maximum-gc-age=24h") {
			t.Errorf("%q was not given the age", r)
		}
	}
}

package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// imageGCFixture points both files at a temporary k3s directory, fixes the
// release, and records restarts instead of performing them.
func imageGCFixture(t *testing.T, release, env string) (dropin, ageconf string, restarts *[]string) {
	t.Helper()
	origDropin, origAge, origRelease, origRestart, origDir := imageGCDropin, imageAgeConf, k3sRelease, systemctlRestart, meshployInstDir
	t.Cleanup(func() {
		imageGCDropin, imageAgeConf, k3sRelease, systemctlRestart, meshployInstDir = origDropin, origAge, origRelease, origRestart, origDir
	})
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "rancher", "k3s"), 0o755); err != nil {
		t.Fatal(err)
	}
	imageGCDropin = filepath.Join(root, "rancher", "k3s", "config.yaml.d", "30-image-cleanup.yaml")
	imageAgeConf = filepath.Join(root, "lib", "kubelet.conf.d", "30-meshploy-image-cleanup.conf")
	k3sRelease = func() string { return release }
	seen := []string{}
	systemctlRestart = func(unit string) error { seen = append(seen, unit); return nil }
	meshployInstDir = t.TempDir()
	if err := os.WriteFile(filepath.Join(meshployInstDir, ".env"), []byte(env), 0o600); err != nil {
		t.Fatal(err)
	}
	return imageGCDropin, imageAgeConf, &seen
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// An upgrade turns the daily cleanup on, once: the next upgrade finds the
// files as they should be and restarts nothing.
func TestCleanImagesDailyWritesOnce(t *testing.T) {
	dropin, ageconf, restarts := imageGCFixture(t, "v1.36.5+k3s1", "DOMAIN=example.com\n")
	for i := 0; i < 2; i++ {
		if err := cleanImagesDaily(); err != nil {
			t.Fatal(err)
		}
	}
	if flags := read(t, dropin); !strings.Contains(flags, "image-gc-high-threshold=80") {
		t.Errorf("drop-in lacks the threshold:\n%s", flags)
	}
	if conf := read(t, ageconf); !strings.Contains(conf, "kind: KubeletConfiguration") || !strings.Contains(conf, "imageMaximumGCAge: 24h") {
		t.Errorf("kubelet configuration lacks the age:\n%s", conf)
	}
	if len(*restarts) != 1 {
		t.Errorf("want one k3s restart, got %v", *restarts)
	}
}

// kubelet has no flag for the age and refuses to start when given one, so the
// age is never a kubelet flag, on any release.
func TestImageAgeIsNeverAKubeletFlag(t *testing.T) {
	if strings.Contains(imageGCWant(), "gc-age") {
		t.Errorf("the k3s drop-in passes the age as a kubelet flag:\n%s", imageGCWant())
	}
}

// A gateway whose drop-in an earlier upgrade wrote with the age as a flag is
// put right: the flag goes, the age moves to kubelet's configuration.
func TestCleanImagesDailyRepairsTheAgeFlag(t *testing.T) {
	dropin, ageconf, restarts := imageGCFixture(t, "v1.36.5+k3s1", "")
	broken := "kubelet-arg+:\n  - \"image-gc-high-threshold=80\"\n  - \"image-gc-low-threshold=70\"\n  - \"image-maximum-gc-age=24h\"\n"
	if err := os.MkdirAll(filepath.Dir(dropin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dropin, []byte(broken), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := cleanImagesDaily(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(read(t, dropin), "gc-age") {
		t.Errorf("the age flag was left in the drop-in")
	}
	if !strings.Contains(read(t, ageconf), "imageMaximumGCAge: 24h") {
		t.Errorf("the age did not move to kubelet's configuration")
	}
	if len(*restarts) != 1 {
		t.Errorf("want one k3s restart, got %v", *restarts)
	}
}

// The age is the operator's to set, and off takes both files away again.
func TestCleanImagesDailyFollowsTheSetting(t *testing.T) {
	dropin, ageconf, restarts := imageGCFixture(t, "v1.36.5+k3s1", "IMAGE_CLEANUP_AFTER=72h\n")
	if err := cleanImagesDaily(); err != nil {
		t.Fatal(err)
	}
	if conf := read(t, ageconf); !strings.Contains(conf, "imageMaximumGCAge: 72h") {
		t.Errorf("kubelet configuration does not carry the operator's age:\n%s", conf)
	}
	if err := setEnvVar("IMAGE_CLEANUP_AFTER", "off"); err != nil {
		t.Fatal(err)
	}
	if err := cleanImagesDaily(); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{dropin, ageconf} {
		if _, err := os.Stat(f); !os.IsNotExist(err) {
			t.Errorf("off left %s: %v", f, err)
		}
	}
	if len(*restarts) != 2 {
		t.Errorf("want a restart for each change, got %v", *restarts)
	}
}

// Kubernetes before 1.30 refuses the age without its feature gate, which
// would stop kubelet starting; such a node gets the thresholds alone.
func TestImageAgeOnlyFromKubernetes130(t *testing.T) {
	dropin, ageconf, _ := imageGCFixture(t, "v1.29.4+k3s1", "")
	if err := cleanImagesDaily(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(read(t, dropin), "image-gc-high-threshold=80") {
		t.Error("1.29 was not given the thresholds")
	}
	if _, err := os.Stat(ageconf); !os.IsNotExist(err) {
		t.Errorf("1.29 was given the age: %v", err)
	}
	for _, r := range []string{"v1.30.0+k3s1", "v1.36.5+k3s1", ""} {
		if !strings.Contains(imageAgeWant("24h", r), "imageMaximumGCAge: 24h") {
			t.Errorf("%q was not given the age", r)
		}
	}
}

package dokploy

import (
	"fmt"
	"strings"
	"testing"

	"github.com/meshploy/apps/cli/internal/migrate/journal"
)

func newMover(t *testing.T, r *fakeRunner) (ImageMover, *journal.Journal) {
	t.Helper()
	j, err := journal.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { j.Close() })
	return ImageMover{Runner: r, Registry: Registry{Endpoint: "100.64.0.1:5000"}, Journal: j}, j
}

// An image Dokploy built on this host exists nowhere else: the cluster would
// find nothing to pull, so it is carried across.
func TestALocallyBuiltImageIsPushed(t *testing.T) {
	r := &fakeRunner{replies: map[string]string{
		"docker image inspect myapp:latest --format {{len .RepoDigests}}": "0\n",
	}}
	m, j := newMover(t, r)

	got, err := m.Push("prepare/image/a1", "", "myapp:latest")
	if err != nil {
		t.Fatal(err)
	}
	if got != "100.64.0.1:5000/myapp:latest" {
		t.Errorf("reference = %q", got)
	}
	if !r.didRun("docker tag myapp:latest 100.64.0.1:5000/myapp:latest") ||
		!r.didRun("docker push 100.64.0.1:5000/myapp:latest") {
		t.Errorf("commands = %v", r.ran)
	}
	entries, _ := journal.Read(j.Dir())
	if len(entries) != 1 || entries[0].Created != got || entries[0].Undo != nil {
		t.Errorf("entry = %+v", entries[0])
	}
}

// An image that came from a registry is left alone: copying postgres:16 into
// our own registry costs disk and buys nothing.
func TestAPulledImageIsLeftAlone(t *testing.T) {
	r := &fakeRunner{replies: map[string]string{
		"docker image inspect postgres:16 --format {{len .RepoDigests}}": "1\n",
	}}
	m, _ := newMover(t, r)

	got, err := m.Push("s", "", "postgres:16")
	if err != nil || got != "postgres:16" {
		t.Fatalf("got %q, %v", got, err)
	}
	if r.didRun("docker push 100.64.0.1:5000/postgres:16") {
		t.Errorf("it was pushed anyway: %v", r.ran)
	}
}

// An image Docker cannot describe is carried rather than hoped for.
func TestAnUndescribableImageIsPushed(t *testing.T) {
	r := &fakeRunner{errs: map[string]error{
		"docker image inspect odd:tag --format {{len .RepoDigests}}": fmt.Errorf("no such image"),
	}}
	m, _ := newMover(t, r)
	if !m.NeedsPush("odd:tag") {
		t.Error("an image Docker will not describe should be pushed")
	}
}

func TestRegistryRefKeepsTheNameReadable(t *testing.T) {
	r := Registry{Endpoint: "100.64.0.1:5000"}
	for in, want := range map[string]string{
		"myapp:latest":                    "100.64.0.1:5000/myapp:latest",
		"myapp":                           "100.64.0.1:5000/myapp:latest",
		"acme/myapp:v2":                   "100.64.0.1:5000/acme/myapp:v2",
		"ghcr.io/acme/myapp:sha-9f3":      "100.64.0.1:5000/acme/myapp:sha-9f3",
		"registry.example.com:5000/x:1.2": "100.64.0.1:5000/x:1.2",
		"localhost:5000/y:dev":            "100.64.0.1:5000/y:dev",
	} {
		if got := r.Ref(in); got != want {
			t.Errorf("%q -> %q, want %q", in, got, want)
		}
	}
}

// A failed push stops that workload rather than creating a service pointing at
// an image nothing can pull.
func TestAFailedPushIsReported(t *testing.T) {
	r := &fakeRunner{
		replies: map[string]string{"docker image inspect myapp:latest --format {{len .RepoDigests}}": "0\n"},
		errs:    map[string]error{"docker push 100.64.0.1:5000/myapp:latest": fmt.Errorf("registry unreachable")},
	}
	m, j := newMover(t, r)

	if _, err := m.Push("s", "", "myapp:latest"); err == nil || !strings.Contains(err.Error(), "unreachable") {
		t.Fatalf("err = %v", err)
	}
	entries, _ := journal.Read(j.Dir())
	if entries[0].Result != journal.Failed || entries[0].Created != "" {
		t.Errorf("entry = %+v", entries[0])
	}
}

// Pushing the same image twice on a resumed run does not repeat the transfer.
func TestPushIsNotRepeatedOnResume(t *testing.T) {
	r := &fakeRunner{replies: map[string]string{
		"docker image inspect myapp:latest --format {{len .RepoDigests}}": "0\n",
	}}
	m, _ := newMover(t, r)

	if _, err := m.Push("s", "", "myapp:latest"); err != nil {
		t.Fatal(err)
	}
	before := len(r.ran)
	got, err := m.Push("s", "", "myapp:latest")
	if err != nil || got != "100.64.0.1:5000/myapp:latest" {
		t.Fatalf("got %q, %v", got, err)
	}
	// It still asks Docker whether the image is local - cheap - but does not
	// tag and push again.
	if r.didRunAfter("docker push 100.64.0.1:5000/myapp:latest", before) {
		t.Errorf("pushed twice: %v", r.ran[before:])
	}
}

// Without a registry configured, images are left as they are rather than
// rewritten to a reference that goes nowhere.
func TestNoRegistryMeansNoRewrite(t *testing.T) {
	m := ImageMover{Runner: &fakeRunner{}}
	got, err := m.Push("s", "", "myapp:latest")
	if err != nil || got != "myapp:latest" {
		t.Fatalf("got %q, %v", got, err)
	}
}

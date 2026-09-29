package service

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	meshdb "github.com/meshploy/packages/db"
)

// A running or failed service takes the change. A stopped one is never started,
// a deploying one is left to the rollout in flight, and a managed database is
// not provisioned again; each is reported instead.
func TestRolloutPlan(t *testing.T) {
	changed := []changedService{
		{ID: uuid.New(), Name: "web", Type: meshdb.ServiceTypeApplication, Status: meshdb.ServiceRunning},
		{ID: uuid.New(), Name: "minio", Type: meshdb.ServiceTypeApplication, Status: meshdb.ServiceFailed},
		{ID: uuid.New(), Name: "worker", Type: meshdb.ServiceTypeApplication, Status: meshdb.ServiceStopped},
		{ID: uuid.New(), Name: "api", Type: meshdb.ServiceTypeApplication, Status: meshdb.ServiceDeploying},
		{ID: uuid.New(), Name: "cache", Type: meshdb.ServiceTypeDatabase, Status: meshdb.ServiceRunning},
	}

	deploy, warnings := rolloutPlan(changed)

	var names []string
	for _, c := range deploy {
		names = append(names, c.Name)
	}
	if strings.Join(names, ",") != "web,minio" {
		t.Errorf("rolled out %v, want web and minio", names)
	}
	if len(warnings) != 3 {
		t.Fatalf("warnings: %v", warnings)
	}
	for _, want := range []string{"worker changed but is not running", "api changed while it was deploying", "cache changed: a managed database"} {
		found := false
		for _, w := range warnings {
			found = found || strings.Contains(w, want)
		}
		if !found {
			t.Errorf("no warning for %q: %v", want, warnings)
		}
	}
}

// A service never deployed was not stopped by anyone: the stack's first
// rollout stopped before it. It goes, changed or not, which is how a stack
// comes up after a failed first rollout; one that ran and was stopped stays.
func TestRolloutPlanStartsWhatNeverRan(t *testing.T) {
	deploy, warnings := rolloutPlan([]changedService{
		{Name: "migrator", Type: meshdb.ServiceTypeApplication, Status: meshdb.ServiceStopped, NeverDeployed: true, Unchanged: true},
		{Name: "s1", Type: meshdb.ServiceTypeApplication, Status: meshdb.ServiceStopped, NeverDeployed: true},
		{Name: "orders", Type: meshdb.ServiceTypeDatabase, Status: meshdb.ServiceStopped, NeverDeployed: true, Unchanged: true},
		{Name: "busy", Type: meshdb.ServiceTypeApplication, Status: meshdb.ServiceDeploying, NeverDeployed: true, Unchanged: true},
		{Name: "worker", Type: meshdb.ServiceTypeApplication, Status: meshdb.ServiceStopped},
	})
	var names []string
	for _, c := range deploy {
		names = append(names, c.Name)
	}
	if strings.Join(names, ",") != "migrator,s1,orders" {
		t.Errorf("rolled out %v", names)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "worker changed but is not running") {
		t.Errorf("warnings %v", warnings)
	}
}

// A stack's rollout builds what its last sync read, for the services built
// from its own repository and branch; everything else builds as before.
func TestStackCommitPinsWhatTheLastSyncRead(t *testing.T) {
	sha := "0123456789abcdef0123456789abcdef01234567"
	stack := meshdb.Stack{GitMode: meshdb.StackGitModeRepo, GitRepo: "https://gitlab.com/acme/pi.git", GitBranch: "main", GitLastSyncSHA: sha}
	bc := func(repo, branch string) meshdb.BuildConfig { return meshdb.BuildConfig{GitRepo: repo, Branch: branch} }

	if got := pinnedCommit(stack, bc("acme/pi", "main")); got != sha {
		t.Errorf("a service built from the stack's repository got %q", got)
	}
	for name, c := range map[string]meshdb.BuildConfig{
		"another repository": bc("acme/other", "main"),
		"another branch":     bc("acme/pi", "develop"),
		"an image":           bc("", ""),
	} {
		if got := pinnedCommit(stack, c); got != "" {
			t.Errorf("%s was pinned: %q", name, got)
		}
	}
	noSync := stack
	noSync.GitLastSyncSHA = ""
	pasted := stack
	pasted.GitMode, pasted.GitRepo = meshdb.StackGitModeRaw, ""
	for name, st := range map[string]meshdb.Stack{"a stack that recorded no commit": noSync, "a pasted stack": pasted} {
		if got := pinnedCommit(st, bc("acme/pi", "main")); got != "" {
			t.Errorf("%s pinned %q", name, got)
		}
	}
}

// A service built from source is rebuilt for new code, not for compose's
// placeholder image name, which never matched what it runs and rebuilt it on
// every apply.
func TestBuildBehind(t *testing.T) {
	sha := "0123456789abcdef0123456789abcdef01234567"
	for _, c := range []struct {
		name              string
		pinned, lastBuilt string
		sync, want        bool
	}{
		{"built at the synced commit", sha, "0123456", false, false},
		{"the same, on a sync of the same commit", sha, "0123456", true, false},
		{"built at another commit", sha, "fedcba9", false, true},
		{"never built", sha, "", false, true},
		{"no commit known, apply again", "", "0123456", false, false},
		{"no commit known, sync", "", "0123456", true, true},
	} {
		if got := buildBehind(c.pinned, c.lastBuilt, c.sync); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

// Nothing changed means nothing rolls out, which is what keeps re-applying an
// unchanged file from restarting a stack. A missing command list and an empty
// one are the same thing.
func TestServiceSpecComparesWhatApplyWrites(t *testing.T) {
	stored := meshdb.Service{
		Image: "nginx:1.27", Replicas: 1,
		CPURequest: "100m", CPULimit: "1000m", MemoryRequest: "256Mi", MemoryLimit: "1Gi",
		EnvVars: meshdb.EncryptedString("A=1"),
		Args:    meshdb.StringArray{"serve"},
	}
	same := serviceSpec{
		Image: "nginx:1.27", Replicas: 1,
		CPURequest: "100m", CPULimit: "1000m", MemoryRequest: "256Mi", MemoryLimit: "1Gi",
		EnvVars: "A=1",
		Command: joinArgs([]string{}), // compose declares no entrypoint
		Args:    joinArgs([]string{"serve"}),
	}
	if storedServiceSpec(stored) != same {
		t.Errorf("an unchanged spec compared as changed:\n%+v\n%+v", storedServiceSpec(stored), same)
	}

	newImage := same
	newImage.Image = "nginx:1.28"
	if storedServiceSpec(stored) == newImage {
		t.Error("a changed image compared as unchanged")
	}
	moreMemory := same
	moreMemory.MemoryLimit = "2Gi"
	if storedServiceSpec(stored) == moreMemory {
		t.Error("a changed memory limit compared as unchanged")
	}
}

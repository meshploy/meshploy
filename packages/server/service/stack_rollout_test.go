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

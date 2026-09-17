package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/meshploy/packages/hostagent"
)

const testRequestID = "3f2b8c1e-9a4d-4e6f-8b2a-1c3d5e7f9a0b"

func newRequestFixture(t *testing.T) {
	t.Helper()
	orig, origRun := hostDir, hostRunRequest
	hostDir = t.TempDir()
	t.Cleanup(func() { hostDir, hostRunRequest = orig, origRun })
	if err := ensureHostDirs(); err != nil {
		t.Fatal(err)
	}
}

func queueHostRequest(t *testing.T, kind, id string) string {
	t.Helper()
	path := filepath.Join(hostagent.InboxDir(hostDir), kind+"-"+id+".json")
	writeTestFile(t, path, `{"id":"`+id+`","type":"`+kind+`","requested_by":"u1","requested_at":"2026-09-17T12:00:00Z"}`)
	return path
}

// A queued plan request is consumed, run once, and leaves its result and a
// finished status where the API reads them.
func TestAgentRunsARequest(t *testing.T) {
	newRequestFixture(t)
	hostRunRequest = func(req hostagent.Request) (string, []byte, os.FileMode, error) {
		return hostagent.PlanFile, []byte(`{"summary":{"moves":1}}`), 0o600, nil
	}
	path := queueHostRequest(t, hostagent.RequestMigratePlan, testRequestID)

	rr := &requestRunner{running: map[string]bool{}}
	var tasks []hostagent.Task
	var mu sync.Mutex
	rr.check(func(task hostagent.Task) { mu.Lock(); tasks = append(tasks, task); mu.Unlock() })
	rr.wg.Wait()

	if fileExists(path) {
		t.Error("request left in the inbox")
	}
	st, err := hostagent.ReadRequestStatus(hostDir, testRequestID)
	if err != nil || st == nil || st.State != hostagent.RequestSucceeded || st.FinishedAt == nil {
		t.Fatalf("status %+v, %v", st, err)
	}
	raw, _, err := hostagent.ReadResult(hostDir, hostagent.PlanFile)
	if err != nil || string(raw) != `{"summary":{"moves":1}}` {
		t.Errorf("result %s, %v", raw, err)
	}
	info, _ := os.Stat(filepath.Join(hostagent.MigrateDir(hostDir), hostagent.PlanFile))
	if info.Mode().Perm() != 0o600 {
		t.Errorf("plan written %v, want 0600", info.Mode().Perm())
	}
	if len(tasks) != 1 || !tasks[0].OK {
		t.Errorf("tasks = %+v", tasks)
	}
}

func TestAgentRecordsAFailedRequest(t *testing.T) {
	newRequestFixture(t)
	hostRunRequest = func(hostagent.Request) (string, []byte, os.FileMode, error) {
		return "", nil, 0, errors.New("Dokploy was not found on this server")
	}
	queueHostRequest(t, hostagent.RequestMigrateDetect, testRequestID)
	rr := &requestRunner{running: map[string]bool{}}
	rr.check(func(hostagent.Task) {})
	rr.wg.Wait()
	st, _ := hostagent.ReadRequestStatus(hostDir, testRequestID)
	if st == nil || st.State != hostagent.RequestFailed || st.Error == "" {
		t.Errorf("status %+v", st)
	}
}

// A second request of a type already running waits in the inbox.
func TestAgentRunsOneOfEachTypeAtATime(t *testing.T) {
	newRequestFixture(t)
	release := make(chan struct{})
	hostRunRequest = func(hostagent.Request) (string, []byte, os.FileMode, error) {
		<-release
		return hostagent.PlanFile, []byte(`{}`), 0o600, nil
	}
	queueHostRequest(t, hostagent.RequestMigratePlan, testRequestID)
	rr := &requestRunner{running: map[string]bool{}}
	rr.check(func(hostagent.Task) {})

	second := queueHostRequest(t, hostagent.RequestMigratePlan, "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee")
	rr.check(func(hostagent.Task) {})
	if !fileExists(second) {
		t.Error("started a second plan while one was running")
	}
	close(release)
	rr.wg.Wait()
	rr.check(func(hostagent.Task) {})
	rr.wg.Wait()
	if fileExists(second) {
		t.Error("the waiting request did not run after the first finished")
	}
}

// Anything in the inbox that is not a valid request is removed, not run.
func TestAgentDiscardsWhatIsNotARequest(t *testing.T) {
	newRequestFixture(t)
	ran := false
	hostRunRequest = func(hostagent.Request) (string, []byte, os.FileMode, error) {
		ran = true
		return "", nil, 0, nil
	}
	bogus := filepath.Join(hostagent.InboxDir(hostDir), "migrate.apply-"+testRequestID+".json")
	writeTestFile(t, bogus, `{"id":"`+testRequestID+`","type":"migrate.apply"}`)
	tmp := filepath.Join(hostagent.InboxDir(hostDir), ".request-123")
	writeTestFile(t, tmp, `partial`)

	var got []hostagent.Task
	rr := &requestRunner{running: map[string]bool{}}
	rr.check(func(task hostagent.Task) { got = append(got, task) })
	rr.wg.Wait()
	if ran || fileExists(bogus) || len(got) != 1 || got[0].OK {
		t.Errorf("ran=%v bogus left=%v tasks=%+v", ran, fileExists(bogus), got)
	}
	if !fileExists(tmp) {
		t.Error("removed a request still being written")
	}
	_ = time.Now
}

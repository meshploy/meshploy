package cmd

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
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
	st, err := hostagent.ReadRequestStatus(hostagent.Local(hostDir), testRequestID)
	if err != nil || st == nil || st.State != hostagent.RequestSucceeded || st.FinishedAt == nil {
		t.Fatalf("status %+v, %v", st, err)
	}
	raw, _, err := hostagent.ReadResult(hostagent.Local(hostDir), hostagent.PlanFile)
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
	st, _ := hostagent.ReadRequestStatus(hostagent.Local(hostDir), testRequestID)
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

// The domain set the API leaves in the inbox is not a request, and is not
// something to clear away: the apply that puts it in service reads it.
func TestAgentLeavesTheDesiredDomainSetInTheInbox(t *testing.T) {
	newRequestFixture(t)
	desired := filepath.Join(hostDir, hostagent.EdgeDesiredFile)
	writeTestFile(t, desired, `{"domains":[]}`)

	var got []hostagent.Task
	rr := &requestRunner{running: map[string]bool{}}
	rr.check(func(task hostagent.Task) { got = append(got, task) })
	rr.wg.Wait()
	if !fileExists(desired) || len(got) != 0 {
		t.Errorf("left=%v tasks=%+v, want the file kept and nothing reported", fileExists(desired), got)
	}
}

// A server.upgrade request leaves the updater the request the console would,
// and only where the updater is on and nothing is already waiting.
func TestAServerUpgradeRequestQueuesTheUpdater(t *testing.T) {
	orig := upgradeDir
	t.Cleanup(func() { upgradeDir = orig })
	upgradeDir = t.TempDir()
	req := hostagent.Request{ID: "6f1c3a52-2d6c-4bde-9a4f-5b7c0e8d1a23", Type: hostagent.RequestServerUpgrade,
		RequestedBy: "owner", RequestedAt: time.Now(), Args: map[string]string{"channel": "edge"}}

	if _, err := queueServerUpgrade(req); err == nil || !strings.Contains(err.Error(), "updater is off") {
		t.Fatalf("with the updater off: %v", err)
	}
	if err := ensureUpgradeDirs(); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(upgradeStateDir(), upgradeEnabledFile), []byte("on\n"), 0o644)
	if _, err := queueServerUpgrade(hostagent.Request{ID: req.ID, Args: map[string]string{"channel": "nightly"}}); err == nil {
		t.Fatal("an unknown channel was queued")
	}
	if _, err := queueServerUpgrade(req); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(upgradeInboxDir(), upgradeRequestFile))
	if err != nil {
		t.Fatal(err)
	}
	var up upgradeRequest
	if json.Unmarshal(b, &up) != nil || up.ID != req.ID || up.Channel != "edge" || up.RequestedBy != "owner" {
		t.Fatalf("queued %s", b)
	}
	if _, err := queueServerUpgrade(req); err == nil {
		t.Fatal("a second upgrade was queued over the first")
	}
}

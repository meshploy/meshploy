package dokploy

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/meshploy/apps/cli/internal/migrate/journal"
)

// fakeStream answers streamed commands, recording what was asked in full: the
// credentials and flags a dump is taken with are the thing worth asserting on.
type fakeStream struct {
	ran     []string
	replies map[string]string
	errs    map[string]error
}

func (f *fakeStream) Stream(stdin io.Reader, stdout io.Writer, name string, args ...string) error {
	cmd := strings.Join(append([]string{name}, args...), " ")
	f.ran = append(f.ran, cmd)
	if stdin != nil {
		// Drain, or the writer on the other side of the pipe never finishes.
		_, _ = io.Copy(io.Discard, stdin)
	}
	for match, err := range f.errs {
		if strings.Contains(cmd, match) {
			return err
		}
	}
	for match, out := range f.replies {
		if strings.Contains(cmd, match) && stdout != nil {
			_, _ = io.WriteString(stdout, out)
			return nil
		}
	}
	return nil
}

func (f *fakeStream) didRun(match string) bool {
	for _, c := range f.ran {
		if strings.Contains(c, match) {
			return true
		}
	}
	return false
}

// fakeKube is the cluster half.
type fakeKube struct {
	ran     []string
	applied []string
	deleted []string
	replies map[string]string
	errs    map[string]error
}

func (k *fakeKube) Exec(namespace, pod string, stdin io.Reader, stdout io.Writer, cmd ...string) error {
	c := namespace + "/" + pod + " " + strings.Join(cmd, " ")
	k.ran = append(k.ran, c)
	if stdin != nil {
		_, _ = io.Copy(io.Discard, stdin)
	}
	for match, err := range k.errs {
		if strings.Contains(c, match) {
			return err
		}
	}
	for match, out := range k.replies {
		if strings.Contains(c, match) && stdout != nil {
			_, _ = io.WriteString(stdout, out)
			return nil
		}
	}
	return nil
}

func (k *fakeKube) Apply(manifest string) error                   { k.applied = append(k.applied, manifest); return nil }
func (k *fakeKube) WaitReady(string, string, time.Duration) error { return nil }
func (k *fakeKube) DeletePod(ns, pod string) error                { k.deleted = append(k.deleted, pod); return nil }

func (k *fakeKube) didRun(match string) bool {
	for _, c := range k.ran {
		if strings.Contains(c, match) {
			return true
		}
	}
	return false
}

// fakeDataAPI is Meshploy's side of a copy.
type fakeDataAPI struct{ pod string }

func (fakeDataAPI) ProjectSlug(string) (string, error)          { return "acme-production", nil }
func (fakeDataAPI) DatabaseSlug(string, string) (string, error) { return "db-a1b2c3", nil }
func (fakeDataAPI) VolumeSlug(string, string) (string, error)   { return "uploads-d4e5f6", nil }
func (fakeDataAPI) ServiceImage(string, string) (string, error) { return "postgres:16", nil }
func (f fakeDataAPI) RunningPod(string, string) (string, error) {
	if f.pod == "" {
		return "db-a1b2c3-77cc", nil
	}
	return f.pod, nil
}

// statefulGroup is the database of the small plan, prepared, with a mover
// wired to fakes.
func statefulGroup(t *testing.T) (DataMover, Group, *fakeStream, *fakeKube, *journal.Journal) {
	t.Helper()
	plan, src := smallPlan(t)
	// Something stored, so the database is one with data to move.
	for i := range plan.Items {
		if plan.Items[i].ID == "d1" {
			plan.Items[i].Details["data_mb"] = "120"
			plan.Items[i].Details["data_move"] = "dump and restore"
		}
	}
	dir := t.TempDir()
	j, err := journal.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { j.Close() })
	if _, err := Prepare(PrepareDeps{Plan: plan, Source: src, API: newFakeAPI(), Journal: j}); err != nil {
		t.Fatal(err)
	}

	stream := &fakeStream{replies: map[string]string{
		"pg_dump": "PGDMP-and-then-some-bytes",
		"psql":    "orders 12\nusers 3\n",
	}}
	runner := &fakeRunner{replies: map[string]string{
		"docker ps -q --no-trunc --filter label=com.docker.swarm.service.name=db-xyz": "cid-db\n",
	}}
	kube := &fakeKube{replies: map[string]string{"psql": "users 3\norders 12\n"}}

	group := Group{ID: "g-shop", Name: "shop", CanMove: true,
		Members: []GroupMember{{Kind: "database", ID: "d1", Name: "db", Project: "Acme · production"}},
		Data:    []GroupData{{Name: "db", MB: 120, Move: "dump and restore"}}}

	m := DataMover{
		Runner: runner, Stream: stream, Kube: kube, API: fakeDataAPI{},
		Journal: j, Plan: plan, Source: src, Dir: dir,
	}
	return m, group, stream, kube, j
}

// A dump is taken with the credentials the data already uses, and what the
// database held is written down beside it: by the time the restore checks,
// Dokploy's database is stopped and cannot be asked again.
func TestDumpReadsWithDokployCredentialsAndRecordsWhatItHeld(t *testing.T) {
	m, group, stream, _, j := statefulGroup(t)

	if err := m.Dump(group, ids(j)); err != nil {
		t.Fatal(err)
	}
	if !stream.didRun("docker exec cid-db pg_dump -Fc -U app -d app") {
		t.Fatalf("dumped with: %v", stream.ran)
	}
	dump, err := os.ReadFile(filepath.Join(m.Dir, "data", "d1.dump"))
	if err != nil || len(dump) == 0 {
		t.Fatalf("dump = %q, %v", dump, err)
	}
	counts, err := os.ReadFile(filepath.Join(m.Dir, "data", "d1.count"))
	if err != nil || !strings.Contains(string(counts), "orders 12") {
		t.Fatalf("counts = %q, %v", counts, err)
	}
	// The dump file holds the data in the clear, so it is written where only
	// root can read it.
	st, err := os.Stat(filepath.Join(m.Dir, "data", "d1.dump"))
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, %v", st.Mode().Perm(), err)
	}
	if e := mustRead(t, j); e[len(e)-1].Action != "dump-database" {
		t.Errorf("journal = %+v", e[len(e)-1])
	}
}

// A copy that cannot be checked has not happened: counts that disagree fail
// the restore, which fails the group, which undoes it.
func TestRestoreFailsWhenWhatArrivedIsNotWhatWasRead(t *testing.T) {
	m, group, _, kube, j := statefulGroup(t)
	if err := m.Dump(group, ids(j)); err != nil {
		t.Fatal(err)
	}
	kube.replies["psql"] = "users 3\norders 11\n" // one row short

	err := m.Restore(group, ids(j))
	if err == nil || !strings.Contains(err.Error(), "does not hold what it held") {
		t.Fatalf("err = %v", err)
	}
	// And the dump is kept, because it is the only copy of what did not arrive.
	if _, statErr := os.Stat(filepath.Join(m.Dir, "data", "d1.dump")); statErr != nil {
		t.Errorf("the dump should still be there: %v", statErr)
	}
}

// Counts that agree, in any order, are a restore that happened - and the dump,
// a copy of the data in the clear, does not outlive it.
func TestRestoreChecksTheCountsAndThenRemovesTheDump(t *testing.T) {
	m, group, _, kube, j := statefulGroup(t)
	if err := m.Dump(group, ids(j)); err != nil {
		t.Fatal(err)
	}
	if err := m.Restore(group, ids(j)); err != nil {
		t.Fatal(err)
	}
	if !kube.didRun("pg_restore -U app -d app --clean") {
		t.Fatalf("restored with: %v", kube.ran)
	}
	if _, err := os.Stat(filepath.Join(m.Dir, "data", "d1.dump")); !os.IsNotExist(err) {
		t.Errorf("the dump should be gone: %v", err)
	}
	if !j.Done("move/g-shop/data/d1") {
		t.Error("the copy should be recorded as done, so a resumed move does not do it twice")
	}
}

// A database whose data moves as files is filled through a helper pod that
// mounts its claim: the claim may be on another node, it is not bound until
// something mounts it, and the workload must not be running while its files
// are replaced.
func TestFilesAreCopiedThroughAHelperPodThatMountsTheClaim(t *testing.T) {
	m, group, stream, kube, j := statefulGroup(t)
	for i := range m.Plan.Items {
		if m.Plan.Items[i].ID == "d1" {
			m.Plan.Items[i].Details["data_move"] = "volume copy, stopped"
		}
	}
	// The source has to exist to be copied, and both sides have to agree on
	// how much arrived.
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "PG_VERSION"), []byte("16\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m.volumePath = func(string) string { return src }
	kube.replies["wc -l"] = "2\n"

	if err := m.CopyFiles(group, ids(j)); err != nil {
		t.Fatal(err)
	}
	if len(kube.applied) != 1 || !strings.Contains(kube.applied[0], "claimName: db-a1b2c3-data") {
		t.Fatalf("applied: %v", kube.applied)
	}
	if !kube.didRun("rm -rf") {
		t.Error("the target should be emptied before the copy, not merged into")
	}
	if !stream.didRun("tar -C "+src+" -cf - .") || !kube.didRun("tar -C /meshploy-data -xf -") {
		t.Fatalf("copied with: %v / %v", stream.ran, kube.ran)
	}
	if len(kube.deleted) == 0 {
		t.Error("the helper pod should not be left holding the claim")
	}
}

// What arrived is counted on both sides, and a copy that landed less than it
// read fails rather than starting a database on half its files.
func TestAShortFileCopyFails(t *testing.T) {
	m, group, _, kube, j := statefulGroup(t)
	for i := range m.Plan.Items {
		if m.Plan.Items[i].ID == "d1" {
			m.Plan.Items[i].Details["data_move"] = "volume copy, stopped"
		}
	}
	src := t.TempDir()
	for _, name := range []string{"a", "b", "c"} {
		if err := os.WriteFile(filepath.Join(src, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	m.volumePath = func(string) string { return src }
	kube.replies["wc -l"] = "2\n" // the directory, and one of three files

	err := m.CopyFiles(group, ids(j))
	if err == nil || !strings.Contains(err.Error(), "entries") {
		t.Fatalf("err = %v", err)
	}
}

// ids is what the move passes each phase: what stage 1 created, from the
// journal.
func ids(j *journal.Journal) IDLookup {
	return func(m GroupMember) (string, string) {
		return j.CreatedBy("prepare/service/" + m.ID), "proj-1"
	}
}

// A Mongo instance holds whatever databases the application made, and the
// platform records none of them - only a root user. So the whole instance
// moves, and asking for a database name that was never recorded would refuse a
// migration that is perfectly able to proceed.
func TestAMongoInstanceMovesWhole(t *testing.T) {
	m, _, _, _, _ := statefulGroup(t)
	m.Source.Rows["mongo"] = []Row{{
		"mongoId": "m1", "name": "eventsdb", "appName": "events-db",
		"databaseUser": "events", "databasePassword": "ev3nts",
	}}
	it := Item{Kind: "database", ID: "m1", Name: "eventsdb",
		Details: map[string]string{"engine": "MongoDB", "app_name": "events-db", "data_mb": "301"}}

	creds, err := m.creds(it)
	if err != nil {
		t.Fatalf("a Mongo with no database name is still movable: %v", err)
	}
	if creds.User != "events" || creds.DB != "" {
		t.Errorf("creds = %+v", creds)
	}

	engine := engineByLabel["MongoDB"]
	dump := strings.Join(engine.Dump(creds), " ")
	if strings.Contains(dump, "-d ") {
		t.Errorf("the whole instance is dumped, not one database: %q", dump)
	}
	restore := strings.Join(engine.Restore(creds), " ")
	for _, keep := range []string{"admin.*", "config.*", "local.*"} {
		if !strings.Contains(restore, keep) {
			t.Errorf("%s belongs to the instance and must not be restored over: %q", keep, restore)
		}
	}

	// An engine that does have a database name still requires one.
	pg := Item{Kind: "database", ID: "d2", Name: "nameless",
		Details: map[string]string{"engine": "Postgres", "app_name": "db-xyz"}}
	m.Source.Rows["postgres"] = []Row{{"postgresId": "d2", "databaseUser": "app"}}
	if _, err := m.creds(pg); err == nil {
		t.Error("a Postgres with no database name cannot be dumped, and should say so")
	}
}

// A host path comes across the same way a volume does, from a directory on
// this machine rather than from Docker's - and only when the operator said so.
func TestABindMountsContentsAreCopiedIn(t *testing.T) {
	m, group, stream, kube, j := statefulGroup(t)
	// A bind mount's source is a directory on this machine, so the test uses
	// one: the copy refuses a path that is not there, which is the right
	// answer for a path somebody moved or mistyped.
	src, left := t.TempDir(), t.TempDir()
	m.Source.Rows["mount"] = []Row{
		{"mountId": "m1", "applicationId": "a1", "type": "bind", "hostPath": src, "mountPath": "/data"},
		{"mountId": "m2", "applicationId": "a1", "type": "bind", "hostPath": left, "mountPath": "/left"},
	}
	group.Members = []GroupMember{{Kind: "application", ID: "a1", Name: "web", Project: "Acme · production"}}
	// Stage 1 made a volume for the path the operator brought, and none for the
	// one they left.
	if err := j.Append(journal.Entry{Step: "prepare/bind/m1", Action: "create-volume",
		Target: "web-srv-web-data", Result: journal.OK, Created: "vol-1"}); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(src, "notes.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	kube.replies["wc -l"] = "2\n"

	if err := m.CopyFiles(group, ids(j)); err != nil {
		t.Fatal(err)
	}
	if !stream.didRun("tar -C " + src + " -cf - .") {
		t.Fatalf("the path itself is the source: %v", stream.ran)
	}
	for _, cmd := range stream.ran {
		if strings.Contains(cmd, left) {
			t.Errorf("a path the operator left behind is not copied: %s", cmd)
		}
	}
	if !kube.didRun("tar -C /meshploy-data -xf -") {
		t.Errorf("it should land in the claim: %v", kube.ran)
	}
}

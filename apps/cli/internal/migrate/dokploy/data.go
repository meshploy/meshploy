package dokploy

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/meshploy/apps/cli/internal/migrate"
	"github.com/meshploy/apps/cli/internal/migrate/journal"
)

// Moving a group's data.
//
// This is the step the whole staged design exists for. Everything else a
// migration does can be done again: a service created wrong is deleted and
// created again, a domain switched too early is switched back. Data copied
// wrong is the one thing that cannot, so the rules here are narrow.
//
//  1. **Nothing is copied while anything can write.** The group's applications
//     are stopped first, and a database is dumped with nothing left writing to
//     it, or stopped entirely and copied file by file.
//  2. **Dokploy's copy is never touched.** Every command on that side reads.
//     What makes a rollback safe is that the data it goes back to was never
//     changed, only read - so a group that fails halfway is put back by
//     starting Dokploy's copy again, and nothing was lost because nothing was
//     moved.
//  3. **A copy that cannot be checked has not happened.** Row counts on both
//     sides for a dump, entry counts on both sides for a volume. A mismatch
//     fails the group, which undoes it.
//
// Two ways to copy, decided by the plan and carried in the item's `data_move`:
// a logical dump for a database small enough that it is quicker than the files
// (under 2 GB), and a file copy of the volume for everything larger, for
// engines with no useful dump, and for an application's own volumes.

// IDLookup maps a group member to what stage 1 created for it: its Meshploy
// service and project. It belongs to the move, which reads the journal, and is
// passed to each phase rather than held here - held here as well, it was left
// unset by the one caller that mattered, and every database looked as though
// stage 1 had never run.
type IDLookup func(GroupMember) (serviceID, projectID string)

// DataAPI is what copying data needs from Meshploy, beyond what the move
// already uses.
type DataAPI interface {
	// ProjectSlug is the Kubernetes namespace a project's workloads run in.
	ProjectSlug(projectID string) (string, error)
	// DatabaseSlug is the name a managed database's objects are made from; its
	// claim is that name with "-data".
	DatabaseSlug(projectID, serviceID string) (string, error)
	// RunningPod is the pod a service is running right now, which is what a
	// restore is executed in.
	RunningPod(projectID, serviceID string) (string, error)
	// VolumeSlug is the claim a Meshploy volume's data lives in.
	VolumeSlug(projectID, volumeID string) (string, error)
	// ServiceImage is what Meshploy will run this workload from. The helper
	// pod that fills a claim uses it, because it is by definition an image
	// this cluster can pull - which the name Docker knows it by on the host is
	// not, when the two keep separate image stores.
	ServiceImage(projectID, serviceID string) (string, error)
}

// KubeSide is the cluster half of a copy. Separated so the data step can be
// tested without a cluster.
type KubeSide interface {
	Exec(namespace, pod string, stdin io.Reader, stdout io.Writer, cmd ...string) error
	Apply(manifest string) error
	WaitReady(namespace, pod string, timeout time.Duration) error
	DeletePod(namespace, pod string) error
}

// DataMover copies one group's data.
type DataMover struct {
	Runner  migrate.Runner
	Stream  migrate.Streamer
	Kube    KubeSide
	API     DataAPI
	Journal *journal.Journal
	// Plan is the confirmed plan, for what each database holds and how it
	// moves. Source is Dokploy read at apply time, for the credentials: those
	// live there and never in plan.json, which is written to disk and shown in
	// a browser.
	Plan   Plan
	Source Source
	// Dir is where dumps are written on the way through, beside the journal.
	Dir string
	// PodTimeout bounds the wait for a helper pod to be ready.
	PodTimeout time.Duration
	// volumePath is where a named Docker volume's files are, a seam so the
	// copy can be tested against a directory rather than /var/lib/docker.
	volumePath func(name string) string
}

// dataDir is where a named Docker volume's files live on this host.
func (m DataMover) dataDir(volume string) string {
	if m.volumePath != nil {
		return m.volumePath(volume)
	}
	return dockerVolumePath(volume)
}

// dbCreds are what both sides of a database copy are reached with. They are the
// same on both sides by construction: stage 1 creates Meshploy's database with
// the credentials the data already uses, because a dump restored under a
// different user or database name is not the same database.
type dbCreds struct{ User, Password, DB string }

// engineData is how one engine is dumped, restored and counted.
//
// Count runs on both sides and its output is compared literally, so it has to
// be deterministic: one line per table or collection, sorted, with its row
// count. Both sides run the same image - stage 1 creates the database on the
// exact tag Dokploy was running - so the same command exists in both.
type engineData struct {
	Dump    func(dbCreds) []string
	Restore func(dbCreds) []string
	Count   func(dbCreds) []string
	// DataPath is where the engine keeps its files, for a copy of the volume
	// rather than a dump.
	DataPath string
	// DumpExt names the file, for an operator looking in the journal directory.
	DumpExt string
	// VolumeOnly is true for an engine with no dump worth doing: Redis holds a
	// cache and writes its own snapshot, so its files are the honest copy.
	VolumeOnly bool
}

// engineByLabel is keyed by the plan's engine label, which is what the item
// carries.
var engineByLabel = map[string]engineData{
	"Postgres": {
		DumpExt:  "dump",
		DataPath: "/var/lib/postgresql/data",
		Dump: func(c dbCreds) []string {
			return []string{"pg_dump", "-Fc", "-U", c.User, "-d", c.DB}
		},
		Restore: func(c dbCreds) []string {
			// Clean first: the database Meshploy created is empty, but a
			// second attempt after a failed one is not, and a restore that
			// appends to half a schema is worse than one that replaces it.
			return []string{"pg_restore", "-U", c.User, "-d", c.DB,
				"--clean", "--if-exists", "--no-owner", "--no-privileges"}
		},
		Count: func(c dbCreds) []string {
			return []string{"psql", "-U", c.User, "-d", c.DB, "-At", "-F", " ", "-c", postgresCountSQL}
		},
	},
	"MySQL": {
		DumpExt:  "sql",
		DataPath: "/var/lib/mysql",
		Dump: func(c dbCreds) []string {
			return []string{"mysqldump", "-u", c.User, "-p" + c.Password,
				"--single-transaction", "--routines", "--triggers", "--no-tablespaces", c.DB}
		},
		Restore: func(c dbCreds) []string {
			return []string{"mysql", "-u", c.User, "-p" + c.Password, c.DB}
		},
		Count: func(c dbCreds) []string {
			return []string{"sh", "-c", mysqlCountScript(c)}
		},
	},
	"MongoDB": {
		DumpExt:  "archive",
		DataPath: "/data/db",
		Dump: func(c dbCreds) []string {
			return []string{"mongodump", "--quiet", "--archive", "-d", c.DB,
				"-u", c.User, "-p", c.Password, "--authenticationDatabase", "admin"}
		},
		Restore: func(c dbCreds) []string {
			// nsInclude rather than --db: the archive already holds one
			// database, and naming it again is what mongorestore refuses.
			return []string{"mongorestore", "--quiet", "--archive", "--drop",
				"--nsInclude", c.DB + ".*",
				"-u", c.User, "-p", c.Password, "--authenticationDatabase", "admin"}
		},
		Count: func(c dbCreds) []string {
			return []string{"sh", "-c", mongoCountScript(c)}
		},
	},
	"Redis": {DataPath: "/data", VolumeOnly: true},
}

// postgresCountSQL lists every table with its exact row count, sorted.
//
// Not pg_stat_user_tables: those counts are estimates that are zero until
// something analyses the tables, so a restore would "verify" against nothing.
// This counts for real, through query_to_xml, which is the one way to get a
// count per table out of a single statement.
const postgresCountSQL = `SELECT table_name,
 (xpath('/row/c/text()', query_to_xml(format('select count(*) as c from %I.%I', table_schema, table_name), false, true, '')))[1]::text::bigint
 FROM information_schema.tables
 WHERE table_schema = 'public' AND table_type = 'BASE TABLE'
 ORDER BY table_name`

func mysqlCountScript(c dbCreds) string {
	q := func(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
	auth := "-u " + q(c.User) + " -p" + q(c.Password)
	return "set -e; mysql " + auth + " -N -B -e " +
		q("SELECT table_name FROM information_schema.tables WHERE table_schema='"+c.DB+"' AND table_type='BASE TABLE' ORDER BY table_name") +
		" | while read t; do n=$(mysql " + auth + " -N -B -e \"SELECT COUNT(*) FROM \\`" + c.DB + "\\`.\\`$t\\`\"); echo \"$t $n\"; done"
}

func mongoCountScript(c dbCreds) string {
	q := func(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
	eval := "db.getCollectionNames().sort().forEach(function(c){ print(c + ' ' + db[c].countDocuments({})) })"
	args := " --quiet -u " + q(c.User) + " -p " + q(c.Password) + " --authenticationDatabase admin " + q(c.DB) + " --eval " + q(eval)
	// mongosh in current images, mongo in older ones. Both take the same shape.
	return "if command -v mongosh >/dev/null 2>&1; then mongosh" + args + "; else mongo" + args + "; fi"
}

// The three phases, in the order a move runs them. They are separate because
// their preconditions are opposites: a dump needs Dokploy's database running,
// a file copy needs it stopped and Meshploy's not started yet, and a restore
// needs Meshploy's running.

// Dump reads every database that moves logically into a file beside the
// journal. Dokploy's database is still running here, with the group's
// applications already stopped, so nothing is writing to what it reads.
func (m DataMover) Dump(group Group, ids IDLookup) error {
	return m.eachDatabase(group, ids, func(member GroupMember, it Item, engine engineData, _, _ string) error {
		if m.byFiles(it) {
			return nil
		}
		step := m.step(group.ID, "dump", member.ID)
		if m.Journal.Done(step) {
			return nil
		}
		creds, err := m.creds(it)
		if err != nil {
			return err
		}
		container, err := m.container(it)
		if err != nil {
			return err
		}
		// What it holds, read before the dump and kept beside it: by the time
		// the restore checks, Dokploy's database is stopped and cannot be
		// asked again.
		counts, err := m.countDokploy(container, engine.Count(creds))
		if err != nil {
			return fmt.Errorf("read what %s holds: %w", it.Name, err)
		}
		if err := m.dump(container, engine.Dump(creds), m.dumpPath(it, engine)); err != nil {
			return err
		}
		if err := os.WriteFile(m.countPath(it), []byte(counts), 0o600); err != nil {
			return err
		}
		return m.Journal.Append(journal.Entry{Step: step, Group: group.ID, Action: "dump-database",
			Target: fmt.Sprintf("%s (%s)", it.Name, tableSummary(counts)), Result: journal.OK})
	})
}

// CopyFiles fills claims from this host's files: databases whose data moves as
// files, and every volume an application of the group mounts.
//
// It runs with both sides stopped - Dokploy's copy so its files are not being
// written, Meshploy's so its claim is free for the helper pod that fills it.
func (m DataMover) CopyFiles(group Group, ids IDLookup) error {
	if err := m.eachDatabase(group, ids, func(member GroupMember, it Item, engine engineData, serviceID, projectID string) error {
		if !m.byFiles(it) {
			return nil
		}
		step := m.step(group.ID, "data", member.ID)
		if m.Journal.Done(step) {
			return nil
		}
		namespace, err := m.API.ProjectSlug(projectID)
		if err != nil {
			return err
		}
		slug, err := m.API.DatabaseSlug(projectID, serviceID)
		if err != nil {
			return err
		}
		image, err := m.API.ServiceImage(projectID, serviceID)
		if err != nil {
			return err
		}
		n, err := m.copyIntoClaim(namespace, slug+"-data", image, m.dataDir(m.appName(it)+"-data"))
		if err != nil {
			return err
		}
		return m.Journal.Append(journal.Entry{Step: step, Group: group.ID, Action: "copy-volume",
			Target: fmt.Sprintf("%s (%d entries)", it.Name, n), Result: journal.OK})
	}); err != nil {
		return err
	}

	// An application's own volumes. Stage 1 created each one and attached it;
	// what it could not do is fill it, because filling it means stopping the
	// application that owns it.
	for _, member := range group.Members {
		if member.Kind == "database" {
			continue
		}
		serviceID, projectID := ids(member)
		if serviceID == "" {
			continue
		}
		for _, mount := range m.volumeMounts(member.ID) {
			step := m.step(group.ID, "data", member.ID+"/"+mount.mountID)
			if m.Journal.Done(step) {
				continue
			}
			volumeID := m.Journal.CreatedBy("prepare/volume/" + mount.mountID)
			if volumeID == "" {
				return fmt.Errorf("%s has no Meshploy volume for %s: run prepare first", member.Name, mount.name)
			}
			namespace, err := m.API.ProjectSlug(projectID)
			if err != nil {
				return err
			}
			claim, err := m.API.VolumeSlug(projectID, volumeID)
			if err != nil {
				return err
			}
			image, err := m.API.ServiceImage(projectID, serviceID)
			if err != nil {
				return err
			}
			n, err := m.copyIntoClaim(namespace, claim, image, m.dataDir(mount.name))
			if err != nil {
				return fmt.Errorf("copy %s: %w", mount.name, err)
			}
			if err := m.Journal.Append(journal.Entry{Step: step, Group: group.ID, Action: "copy-volume",
				Target: fmt.Sprintf("%s (%d entries)", mount.name, n), Result: journal.OK}); err != nil {
				return err
			}
		}
	}
	return nil
}

// Restore loads each dump into the Meshploy database now running, and checks
// that what arrived is what was read.
func (m DataMover) Restore(group Group, ids IDLookup) error {
	return m.eachDatabase(group, ids, func(member GroupMember, it Item, engine engineData, serviceID, projectID string) error {
		if m.byFiles(it) {
			return nil
		}
		step := m.step(group.ID, "data", member.ID)
		if m.Journal.Done(step) {
			return nil
		}
		creds, err := m.creds(it)
		if err != nil {
			return err
		}
		namespace, err := m.API.ProjectSlug(projectID)
		if err != nil {
			return err
		}
		pod, err := m.API.RunningPod(projectID, serviceID)
		if err != nil {
			return err
		}
		if err := m.restore(namespace, pod, engine.Restore(creds), m.dumpPath(it, engine)); err != nil {
			return err
		}
		before, err := os.ReadFile(m.countPath(it))
		if err != nil {
			return err
		}
		after, err := m.countMeshploy(namespace, pod, engine.Count(creds))
		if err != nil {
			return fmt.Errorf("read what %s holds after the restore: %w", it.Name, err)
		}
		if string(before) != after {
			return fmt.Errorf("%s does not hold what it held: Dokploy had [%s], Meshploy has [%s]",
				it.Name, oneLine(string(before)), oneLine(after))
		}
		// The dump has served its purpose and is a copy of the data in the
		// clear. The journal keeps what happened; it does not keep the data.
		_ = os.Remove(m.dumpPath(it, engine))
		_ = os.Remove(m.countPath(it))
		return m.Journal.Append(journal.Entry{Step: step, Group: group.ID, Action: "restore-database",
			Target: fmt.Sprintf("%s (%s)", it.Name, tableSummary(after)), Result: journal.OK})
	})
}

// eachDatabase runs fn for every database of the group that carries data,
// turning a failure into a journalled one so the operator sees which database
// stopped the move.
func (m DataMover) eachDatabase(group Group, ids IDLookup, fn func(GroupMember, Item, engineData, string, string) error) error {
	for _, member := range group.Members {
		if member.Kind != "database" {
			continue
		}
		it, ok := m.item(member.ID)
		if !ok || it.Details["data_mb"] == "" {
			continue // nothing stored: an empty database is created empty
		}
		engine, ok := engineByLabel[it.Details["engine"]]
		if !ok {
			return fmt.Errorf("%s is not an engine this step can copy", it.Details["engine"])
		}
		serviceID, projectID := "", ""
		if ids != nil {
			serviceID, projectID = ids(member)
		}
		if serviceID == "" {
			return fmt.Errorf("%s has no Meshploy copy: run prepare first", member.Name)
		}
		if err := fn(member, it, engine, serviceID, projectID); err != nil {
			_ = m.Journal.Append(journal.Entry{Step: m.step(group.ID, "data", member.ID), Group: group.ID,
				Action: "copy-data", Target: member.Name, Result: journal.Failed, Error: err.Error()})
			return fmt.Errorf("copy %s: %w", member.Name, err)
		}
	}
	return nil
}

func (m DataMover) step(group, what, id string) string {
	return fmt.Sprintf("move/%s/%s/%s", group, what, id)
}

func (m DataMover) dumpPath(it Item, e engineData) string {
	return filepath.Join(m.Dir, "data", it.ID+"."+e.DumpExt)
}

func (m DataMover) countPath(it Item) string {
	return filepath.Join(m.Dir, "data", it.ID+".count")
}

// volumeMount is one named Docker volume an application mounts.
type volumeMount struct{ mountID, name string }

// volumeMounts are the named volumes this workload mounts, read from Dokploy
// at apply time. Bind mounts are not here: whether a host path moves is the
// operator's decision, and the plan refuses one until that is built.
func (m DataMover) volumeMounts(itemID string) []volumeMount {
	var out []volumeMount
	for _, r := range m.Source.Rows["mount"] {
		if r.Str("type") != "volume" || r.Str("volumeName") == "" {
			continue
		}
		if !mountOwnedBy(r, itemID) {
			continue
		}
		out = append(out, volumeMount{mountID: r.Str("mountId"), name: r.Str("volumeName")})
	}
	return out
}

// mountOwnedBy reports whether this mount row belongs to that workload. Dokploy
// keeps one mount table with a column per kind of owner.
func mountOwnedBy(r Row, itemID string) bool {
	for _, key := range []string{"applicationId", "composeId", "postgresId", "mysqlId", "mariadbId", "mongoId", "redisId"} {
		if r.Str(key) == itemID {
			return true
		}
	}
	return false
}

func (m DataMover) appName(it Item) string {
	if n := it.Details["app_name"]; n != "" {
		return n
	}
	return it.Name
}

// WantsStoppedSource reports whether this group has data that can only be
// copied with Dokploy's database stopped, which decides the order the move
// stops things in.
func (m DataMover) WantsStoppedSource(group Group) bool {
	for _, member := range group.Members {
		if it, ok := m.item(member.ID); ok && m.byFiles(it) {
			return true
		}
	}
	return false
}

// byFiles reports whether this item's data moves as files rather than a dump.
func (m DataMover) byFiles(it Item) bool {
	if it.Kind != "database" || it.Details["data_mb"] == "" {
		return false
	}
	if e, ok := engineByLabel[it.Details["engine"]]; ok && e.VolumeOnly {
		return true
	}
	return strings.HasPrefix(it.Details["data_move"], "volume")
}

// dump reads Dokploy's database into a file beside the journal.
//
// Through a file rather than straight into the restore: a dump that fails
// halfway must not have already been half-applied to the other side, and an
// operator whose restore failed has the dump to try again with.
func (m DataMover) dump(container string, cmd []string, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	args := append([]string{"exec", container}, cmd...)
	if err := m.Stream.Stream(nil, f, "docker", args...); err != nil {
		return fmt.Errorf("dump: %w", err)
	}
	st, err := f.Stat()
	if err != nil {
		return err
	}
	if st.Size() == 0 {
		return fmt.Errorf("dump: the database produced nothing")
	}
	return nil
}

func (m DataMover) restore(namespace, pod string, cmd []string, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := m.Kube.Exec(namespace, pod, f, nil, cmd...); err != nil {
		return fmt.Errorf("restore: %w", err)
	}
	return nil
}

func (m DataMover) countDokploy(container string, cmd []string) (string, error) {
	var out strings.Builder
	if err := m.Stream.Stream(nil, &out, "docker", append([]string{"exec", container}, cmd...)...); err != nil {
		return "", err
	}
	return normalizeCount(out.String()), nil
}

func (m DataMover) countMeshploy(namespace, pod string, cmd []string) (string, error) {
	var out strings.Builder
	if err := m.Kube.Exec(namespace, pod, nil, &out, cmd...); err != nil {
		return "", err
	}
	return normalizeCount(out.String()), nil
}

// copyIntoClaim copies a directory on this host into a Kubernetes claim.
//
// Through a helper pod that mounts the claim, rather than writing into the
// provisioner's directory on this host: the claim may be on another node, it
// may not be bound until something mounts it, and the workload that will use
// it must not be running while its files are replaced. A pod solves all three,
// and it runs the workload's own image, so nothing extra has to be pulled onto
// a server mid-migration.
//
// It returns the number of entries the copy landed.
func (m DataMover) copyIntoClaim(namespace, claim, image, srcDir string) (int, error) {
	if st, err := os.Stat(srcDir); err != nil || !st.IsDir() {
		return 0, fmt.Errorf("%s is not there to copy", srcDir)
	}
	want, err := m.countEntries(srcDir)
	if err != nil {
		return 0, err
	}

	pod := "meshploy-migrate-" + claim
	if len(pod) > 60 {
		pod = pod[:60]
	}
	// A helper left behind by an interrupted run holds the claim, so the first
	// thing is to be sure there is not one.
	_ = m.Kube.DeletePod(namespace, pod)
	if err := m.Kube.Apply(copyPodManifest(namespace, pod, claim, image)); err != nil {
		return 0, fmt.Errorf("start the copy helper: %w", err)
	}
	defer func() { _ = m.Kube.DeletePod(namespace, pod) }()

	timeout := m.PodTimeout
	if timeout == 0 {
		timeout = 3 * time.Minute
	}
	if err := m.Kube.WaitReady(namespace, pod, timeout); err != nil {
		return 0, fmt.Errorf("the copy helper did not start: %w", err)
	}

	// Empty it first: the workload may have started once and written its own
	// files there, and a copy merged into those is neither one database nor
	// the other.
	if err := m.Kube.Exec(namespace, pod, nil, nil, "sh", "-c",
		"set -e; cd "+copyMount+" && rm -rf ./* ./.[!.]* 2>/dev/null || true"); err != nil {
		return 0, fmt.Errorf("clear the target: %w", err)
	}

	pr, pw := io.Pipe()
	errs := make(chan error, 1)
	go func() {
		err := m.Stream.Stream(nil, pw, "tar", "-C", srcDir, "-cf", "-", ".")
		_ = pw.CloseWithError(err)
		errs <- err
	}()
	unpack := m.Kube.Exec(namespace, pod, pr, nil, "tar", "-C", copyMount, "-xf", "-")
	packed := <-errs
	if packed != nil {
		return 0, fmt.Errorf("read %s: %w", srcDir, packed)
	}
	if unpack != nil {
		return 0, fmt.Errorf("write into %s: %w", claim, unpack)
	}

	// What landed, counted the same way on both sides. tar fails loudly on a
	// short write, so this is the second pair of eyes rather than the only one.
	var out strings.Builder
	if err := m.Kube.Exec(namespace, pod, nil, &out, "sh", "-c", "find "+copyMount+" | wc -l"); err != nil {
		return 0, fmt.Errorf("check the copy: %w", err)
	}
	got := atoiOr(strings.TrimSpace(out.String()), -1)
	if got != want {
		return 0, fmt.Errorf("%s holds %d entries, %s held %d", claim, got, srcDir, want)
	}
	return got, nil
}

// copyMount is where a helper pod mounts the claim it is filling. Its own path,
// not the engine's: the helper is not running the engine, and a fixed path
// keeps the commands the same for every workload.
const copyMount = "/meshploy-data"

func copyPodManifest(namespace, pod, claim, image string) string {
	return fmt.Sprintf(`apiVersion: v1
kind: Pod
metadata:
  name: %s
  namespace: %s
  labels:
    meshploy.com/role: migration-copy
spec:
  restartPolicy: Never
  terminationGracePeriodSeconds: 0
  containers:
    - name: copy
      image: %s
      command: ["sh", "-c", "sleep 3600"]
      volumeMounts:
        - name: data
          mountPath: %s
  volumes:
    - name: data
      persistentVolumeClaim:
        claimName: %s
`, pod, namespace, image, copyMount, claim)
}

func (m DataMover) countEntries(dir string) (int, error) {
	n := 0
	err := filepath.Walk(dir, func(string, os.FileInfo, error) error {
		n++
		return nil
	})
	return n, err
}

// container is the Docker container Dokploy runs this database in, right now.
//
// Looked up rather than remembered: a Swarm service's task is replaced
// whenever it restarts, so the container recorded when the plan was made is
// usually not the one running when the group moves.
func (m DataMover) container(it Item) (string, error) {
	appName := it.Details["app_name"]
	if appName == "" {
		appName = it.Name
	}
	out, err := m.Runner.Output("docker", "ps", "-q", "--no-trunc",
		"--filter", "label=com.docker.swarm.service.name="+appName)
	if err != nil {
		return "", err
	}
	if id := firstLine(out); id != "" {
		return id, nil
	}
	out, err = m.Runner.Output("docker", "ps", "-q", "--no-trunc", "--filter", "name=^"+appName+"$")
	if err != nil {
		return "", err
	}
	if id := firstLine(out); id != "" {
		return id, nil
	}
	return "", fmt.Errorf("%s is not running, and a dump has to be read from a running database", appName)
}

// creds are the credentials the data already uses, read from Dokploy at apply
// time.
func (m DataMover) creds(it Item) (dbCreds, error) {
	for _, e := range engines {
		for _, r := range m.Source.Rows[e.table] {
			if idOf(r) != it.ID {
				continue
			}
			c := dbCreds{User: r.Str("databaseUser"), Password: r.Str("databasePassword"), DB: r.Str("databaseName")}
			if c.DB == "" {
				c.DB = it.Details["database"]
			}
			if c.User == "" || c.DB == "" {
				return c, fmt.Errorf("%s has no user or database name recorded in Dokploy", it.Name)
			}
			return c, nil
		}
	}
	return dbCreds{}, fmt.Errorf("%s is no longer in Dokploy's database", it.Name)
}

func (m DataMover) item(id string) (Item, bool) {
	for _, it := range m.Plan.Items {
		if it.ID == id {
			return it, true
		}
	}
	return Item{}, false
}

// dockerVolumePath is where Docker keeps a named volume's files.
func dockerVolumePath(name string) string {
	return filepath.Join("/var/lib/docker/volumes", name, "_data")
}

// normalizeCount makes two count outputs comparable: the same lines, sorted,
// without the blank ones a shell loop leaves behind.
func normalizeCount(s string) string {
	var lines []string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

func oneLine(s string) string {
	return strings.Join(strings.Split(s, "\n"), ", ")
}

// tableSummary is what the journal records: how much arrived, not what it is.
func tableSummary(counts string) string {
	if counts == "" {
		return "no tables"
	}
	lines := strings.Split(counts, "\n")
	rows := 0
	for _, l := range lines {
		_, n, ok := strings.Cut(l, " ")
		if !ok {
			continue
		}
		rows += atoiOr(strings.TrimSpace(n), 0)
	}
	return fmt.Sprintf("%d table(s), %d row(s)", len(lines), rows)
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	line, _, _ := strings.Cut(s, "\n")
	return strings.TrimSpace(line)
}

// Package journal records what a migration did, in order, with how to undo it.
//
// Three things depend on it. **Rollback** replays it backwards. **Resume**
// reads it forwards: a step that already succeeded is skipped, so an apply that
// failed halfway carries on from the failure rather than starting again.
// **The operator** reads it to see what happened to their server.
//
// It is append-only and flushed on every write, because the thing it protects
// against is the process dying in the middle. A line that cannot be parsed is
// skipped rather than fatal: a truncated last line is what a power cut looks
// like, and it must not make the whole record unreadable.
package journal

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// FileName is the journal inside the migration's state directory.
const FileName = "journal.jsonl"

// Result of a step.
const (
	OK      = "ok"
	Failed  = "failed"
	Skipped = "skipped"
	// Undone marks a step a rollback reversed. It clears the step's done mark,
	// so a later run does the work again rather than skipping what is no longer
	// there - a move that was rolled back and tried again waited forever for a
	// service it never restarted.
	Undone = "undone"
)

// Entry is one step the migration took.
type Entry struct {
	At time.Time `json:"at"`
	// Step is a stable identity for this piece of work - "prepare/service/<id>"
	// - so a re-run can tell what it already did. Unique within a migration.
	Step string `json:"step"`
	// Group is the group this step belongs to, empty for stage 1 and cutover.
	// Rollback of one group replays only its own steps.
	Group string `json:"group,omitempty"`
	// Action and Target are what happened, for a person reading it.
	Action string `json:"action"`
	Target string `json:"target,omitempty"`
	Result string `json:"result"`
	Error  string `json:"error,omitempty"`
	// Created is the id of the thing this step made, where it made one.
	//
	// Without it a resumed run knows a project was created but not which one,
	// and everything that belongs inside it fails. The journal has to carry
	// enough to carry on, not only enough to undo.
	Created string `json:"created,omitempty"`
	// Undo is how to reverse this step. Absent when there is nothing to undo -
	// a read, or a step that made no change.
	Undo *Undo `json:"undo,omitempty"`
}

// Undo is one reversal. Kind names the operation and Args carries what it
// needs; both are read by the rollback runner, which refuses a kind it does
// not know rather than guessing.
type Undo struct {
	Kind string            `json:"kind"`
	Args map[string]string `json:"args,omitempty"`
}

// Undo kinds. Each one is implemented by the rollback runner, and adding a kind
// without implementing it there is caught by a test.
const (
	// UndoRestoreFile writes Args["path"] back to the content stored in
	// Args["backup"], a file beside the journal.
	UndoRestoreFile = "restore-file"
	// UndoRemoveFile deletes Args["path"], for a file the migration created.
	UndoRemoveFile = "remove-file"
	// UndoScaleService sets Swarm service Args["service"] back to
	// Args["replicas"].
	UndoScaleService = "scale-service"
	// UndoStartContainer starts container Args["container"] again.
	UndoStartContainer = "start-container"
	// UndoStopContainer stops container Args["container"], for one the
	// migration started - Meshploy's own edge, which has to come off the ports
	// before the old one can go back on them.
	UndoStopContainer = "stop-container"
	// UndoStopService stops the Meshploy service Args["service_id"] in project
	// Args["project_id"] - its copy, left in place so a second attempt reuses it.
	UndoStopService = "stop-service"
	// UndoPauseRoute puts route Args["route_id"] back to paused.
	UndoPauseRoute = "pause-route"
	// UndoPauseTCPRoute closes the gateway port of TCP route Args["route_id"].
	UndoPauseTCPRoute = "pause-tcp-route"
	// UndoStartUnit starts systemd unit Args["unit"] again, for a custom edge
	// that was stopped at cutover.
	UndoStartUnit = "start-unit"
)

// Journal is an open journal file.
type Journal struct {
	mu      sync.Mutex
	dir     string
	file    *os.File
	done    map[string]bool
	created map[string]string
}

// Open reads the journal in dir and opens it for appending, creating the
// directory if it is not there.
func Open(dir string) (*Journal, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	entries, err := Read(dir)
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, FileName), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	j := &Journal{dir: dir, file: f, done: map[string]bool{}, created: map[string]string{}}
	// Last entry wins, exactly as Append decides it: a step that succeeded and
	// was then undone, or succeeded and later failed, is not done.
	for _, e := range entries {
		j.mark(e)
	}
	return j, nil
}

func (j *Journal) Close() error { return j.file.Close() }

// Dir is where the journal and its backups live.
func (j *Journal) Dir() string { return j.dir }

// Append writes one entry and flushes it. A step that fails clears its done
// mark, so a resume retries it.
func (j *Journal) Append(e Entry) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if e.At.IsZero() {
		e.At = time.Now().UTC()
	}
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	if _, err := j.file.Write(append(b, '\n')); err != nil {
		return err
	}
	if err := j.file.Sync(); err != nil {
		return err
	}
	j.mark(e)
	return nil
}

// mark records what an entry means for a resume. Called under the lock by
// Append, and by Open while nothing else holds the journal.
func (j *Journal) mark(e Entry) {
	switch e.Result {
	case OK, Skipped:
		j.done[e.Step] = true
		if e.Created != "" {
			j.created[e.Step] = e.Created
		}
	default:
		delete(j.done, e.Step)
		delete(j.created, e.Step)
	}
}

// Done reports whether this step already succeeded, so a resumed run skips it.
func (j *Journal) Done(step string) bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.done[step]
}

// CreatedBy returns what a step made, for a resumed run that skips it but still
// needs the id.
func (j *Journal) CreatedBy(step string) string {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.created[step]
}

// Backup stores content beside the journal and returns the path, for an undo
// that has to put a file back the way it was.
func (j *Journal) Backup(name string, content []byte) (string, error) {
	path := filepath.Join(j.dir, "backup", name)
	// name may carry directories - an edge's configuration is copied with its
	// own layout, so it can be put back the way it was found.
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// Read returns every entry in dir's journal, oldest first. A missing journal is
// an empty list, not an error: nothing has happened yet.
func Read(dir string) ([]Entry, error) {
	f, err := os.Open(filepath.Join(dir, FileName))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []Entry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var e Entry
		if err := json.Unmarshal(line, &e); err != nil {
			// A half-written last line is what a power cut looks like. The rest
			// of the record is still true.
			continue
		}
		out = append(out, e)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read journal: %w", err)
	}
	return out, nil
}

// Undoable returns the steps to reverse, newest first: the successful ones that
// carry an undo. Passing a group returns only that group's steps, which is what
// rolling back one group means.
func Undoable(entries []Entry, group string) []Entry {
	var out []Entry
	undone := map[string]bool{}
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		// Newest first, so a step's reversal is seen before the step itself: a
		// rollback that ran already must not run again, on a second rollback or
		// on a rollback of everything after one group was put back.
		if e.Result == Undone {
			undone[e.Step] = true
			continue
		}
		if e.Result != OK || e.Undo == nil || undone[e.Step] {
			continue
		}
		if group != "" && e.Group != group {
			continue
		}
		out = append(out, e)
	}
	return out
}

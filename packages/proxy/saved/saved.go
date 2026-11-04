// Package saved keeps the edge's last good state on disk, so the edge serves
// without the database.
//
// Each part of the edge (routes, the mesh gate, TCP ports) loads from the
// database and saves what it loaded here. A proxy that starts while the
// database is down, or never reaches it, serves the copy instead, until a
// load succeeds. The database stays the only source: nothing here is written
// back to it, and a copy is only ever replaced by a newer load.
package saved

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Format is the layout of every file here. A proxy reads only its own
// format: a copy written by another version is ignored rather than misread,
// and the next load replaces it. Raise it whenever a saved type changes shape.
const Format = 1

type file struct {
	Format  int             `json:"format"`
	SavedAt time.Time       `json:"saved_at"`
	Data    json.RawMessage `json:"data"`
}

// Dir is the directory the copies live in. A nil Dir saves and finds
// nothing, as a proxy run without one does.
type Dir struct {
	path string
	mu   sync.Mutex
	last map[string][sha256.Size]byte // what each name last held, to skip unchanged writes
}

// Open is the directory at path, created when missing; nil for an empty path.
func Open(path string) (*Dir, error) {
	if path == "" {
		return nil, nil
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return nil, err
	}
	return &Dir{path: path, last: map[string][sha256.Size]byte{}}, nil
}

// Save replaces name's copy with v. Unchanged data is not written again, so a
// reload every few seconds costs no disk writes. The file is replaced whole:
// a crash leaves the old copy or the new one, never part of either.
func (d *Dir) Save(name string, v any) error {
	if d == nil {
		return nil
	}
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(data)
	d.mu.Lock()
	defer d.mu.Unlock()
	if prev, ok := d.last[name]; ok && prev == sum {
		return nil
	}
	out, err := json.Marshal(file{Format: Format, SavedAt: time.Now().UTC(), Data: data})
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(d.path, "."+name+"-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // gone already once renamed
	if _, err := tmp.Write(out); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), d.file(name)); err != nil {
		return err
	}
	d.last[name] = sum
	return nil
}

// ErrNone is a copy that is missing, or written in another format.
var ErrNone = errors.New("no saved copy")

// Load reads name's copy into v, and says when it was saved.
func (d *Dir) Load(name string, v any) (time.Time, error) {
	if d == nil {
		return time.Time{}, ErrNone
	}
	raw, err := os.ReadFile(d.file(name))
	if errors.Is(err, os.ErrNotExist) {
		return time.Time{}, ErrNone
	}
	if err != nil {
		return time.Time{}, err
	}
	var f file
	if err := json.Unmarshal(raw, &f); err != nil {
		return time.Time{}, fmt.Errorf("%s: %w", d.file(name), err)
	}
	if f.Format != Format {
		return time.Time{}, ErrNone
	}
	dec := json.NewDecoder(bytes.NewReader(f.Data))
	if err := dec.Decode(v); err != nil {
		return time.Time{}, fmt.Errorf("%s: %w", d.file(name), err)
	}
	return f.SavedAt, nil
}

// Path is where the copies are.
func (d *Dir) Path() string { return d.path }

func (d *Dir) file(name string) string { return filepath.Join(d.path, name+".json") }

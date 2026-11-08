package hostagent

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"sort"
	"strings"
	"time"
)

// Folder is the host agent's directory as the API reaches it. On a gateway
// it is a directory on the same machine (Local). On an edge whose API is
// elsewhere, the cluster agent on the edge reads and writes it for the API,
// and the host agent itself is the same either way.
//
// Names are slash-separated and relative to the directory, such as
// "state/agent.json". The API reads state/ and the requests waiting in
// inbox/, and writes into inbox/ only.
type Folder interface {
	// Read returns the regular file at name and when it last changed. An
	// error wrapping fs.ErrNotExist is a file that is not there; a link, a
	// folder or a file over limit is refused.
	Read(name string, limit int64) ([]byte, time.Time, error)
	// List names the files in the folder at name, sorted. A folder that is
	// not there has none.
	List(name string) ([]string, error)
	// Put leaves data at name, whole: a reader never sees half of it. A
	// private file is readable by root only.
	Put(name string, data []byte, private bool) error
}

// ErrNotAFile is a name that is a link, a folder or too large to be read as
// a report.
var ErrNotAFile = errors.New("not a file the host agent wrote")

// CleanName checks a name a Folder is asked for: relative, slash-separated,
// with no step outside the folder.
func CleanName(name string) (string, error) {
	if name == "" || !fs.ValidPath(name) || name == "." {
		return "", fmt.Errorf("%q is not a name inside the host agent's folder", name)
	}
	return name, nil
}

// Local is the host agent's directory on this machine, at the path it names.
// Every name is opened inside it (os.Root), so no link leads out of it.
type Local string

func (l Local) root() (*os.Root, error) { return os.OpenRoot(string(l)) }

func (l Local) Read(name string, limit int64) ([]byte, time.Time, error) {
	name, err := CleanName(name)
	if err != nil {
		return nil, time.Time{}, err
	}
	r, err := l.root()
	if err != nil {
		return nil, time.Time{}, err
	}
	defer r.Close()
	info, err := r.Lstat(name)
	if err != nil {
		return nil, time.Time{}, err
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		return nil, time.Time{}, fmt.Errorf("%s: %w", name, ErrNotAFile)
	}
	f, err := r.Open(name)
	if err != nil {
		return nil, time.Time{}, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, time.Time{}, err
	}
	if int64(len(b)) > limit {
		return nil, time.Time{}, fmt.Errorf("%s: %w", name, ErrNotAFile)
	}
	return b, info.ModTime().UTC(), nil
}

func (l Local) List(name string) ([]string, error) {
	name, err := CleanName(name)
	if err != nil {
		return nil, err
	}
	r, err := l.root()
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer r.Close()
	entries, err := fs.ReadDir(r.FS(), name)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.Type().IsRegular() {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out, nil
}

func (l Local) Put(name string, data []byte, private bool) error {
	name, err := CleanName(name)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(string(l), 0o755); err != nil {
		return err
	}
	r, err := l.root()
	if err != nil {
		return err
	}
	defer r.Close()
	dir := path.Dir(name)
	if dir != "." {
		if err := r.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	mode := os.FileMode(0o644)
	if private {
		mode = 0o600
	}
	var suffix [6]byte
	_, _ = rand.Read(suffix[:])
	tmp := path.Join(dir, "."+strings.TrimSuffix(path.Base(name), ".json")+"-"+hex.EncodeToString(suffix[:]))
	f, err := r.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	defer r.Remove(tmp)
	// The mode is set again: the process's umask may have narrowed it.
	if err := f.Chmod(mode); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return r.Rename(tmp, name)
}

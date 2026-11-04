package saved

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type thing struct {
	Name  string
	Count int
}

// A copy reads back as it was saved; saving the same data again leaves the
// file alone; a copy in another format, or none, is ErrNone.
func TestSaveAndLoad(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "edge"))
	if err != nil {
		t.Fatal(err)
	}
	var got thing
	if _, err := d.Load("routes", &got); !errors.Is(err, ErrNone) {
		t.Fatalf("nothing saved yet: %v", err)
	}
	if err := d.Save("routes", thing{"a", 1}); err != nil {
		t.Fatal(err)
	}
	at, err := d.Load("routes", &got)
	if err != nil || got != (thing{"a", 1}) || time.Since(at) > time.Minute {
		t.Fatalf("got %+v at %s, %v", got, at, err)
	}
	info, _ := os.Stat(d.file("routes"))
	os.Chtimes(d.file("routes"), time.Unix(0, 0), time.Unix(0, 0))
	if err := d.Save("routes", thing{"a", 1}); err != nil {
		t.Fatal(err)
	}
	if again, _ := os.Stat(d.file("routes")); !again.ModTime().Equal(time.Unix(0, 0)) {
		t.Error("unchanged data was written again")
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("the copy is readable by others: %v", info.Mode().Perm())
	}
	if err := d.Save("routes", thing{"b", 2}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Load("routes", &got); err != nil || got != (thing{"b", 2}) {
		t.Fatalf("changed data: %+v %v", got, err)
	}
	os.WriteFile(d.file("routes"), []byte(`{"format":999,"data":{"Name":"x"}}`), 0o600)
	if _, err := d.Load("routes", &got); !errors.Is(err, ErrNone) {
		t.Errorf("another format was read: %v", err)
	}
	if left, _ := filepath.Glob(filepath.Join(d.path, ".*")); len(left) != 0 {
		t.Errorf("temporary files left: %v", left)
	}

	var none *Dir
	if err := none.Save("routes", thing{}); err != nil {
		t.Error(err)
	}
	if _, err := none.Load("routes", &got); !errors.Is(err, ErrNone) {
		t.Error("a missing directory found something")
	}
}

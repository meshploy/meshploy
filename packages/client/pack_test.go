package client

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func archived(t *testing.T, archive []byte) []string {
	t.Helper()
	zr, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(zr)
	var names []string
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if h.Typeflag != tar.TypeDir {
			names = append(names, h.Name)
		}
	}
	sort.Strings(names)
	return names
}

// What the build does not need, and what holds secrets, stays behind; what
// the ignore files name does too, and a ! takes a name back.
func TestPackLeavesOutWhatTheBuildDoesNotNeed(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "my-app")
	writeTree(t, dir, map[string]string{
		"package.json":            "{}",
		"src/index.js":            "x",
		"src/debug.log":           "log",
		"src/keep.log":            "kept",
		"dist/bundle.js":          "built",
		"node_modules/a/index.js": "dep",
		".git/HEAD":               "ref",
		".env":                    "SECRET=1",
		".env.production":         "SECRET=2",
		".env.example":            "SECRET=",
		"notes/todo.md":           "later",
		".gitignore":              "*.log\n!keep.log\n/dist/\n",
		".meshployignore":         "notes/\n",
	})
	p, err := PackFolder(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{".env.example", ".gitignore", ".meshployignore", "package.json", "src/index.js", "src/keep.log"}
	if got := archived(t, p.Archive); !equal(got, want) {
		t.Fatalf("archived %v, want %v", got, want)
	}
	if p.Name != "my-app" || p.Files != len(want) {
		t.Fatalf("name %q files %d", p.Name, p.Files)
	}
	sort.Strings(p.Skipped)
	if !equal(p.Skipped, []string{".env", ".env.production", ".git", "node_modules"}) {
		t.Fatalf("skipped %v", p.Skipped)
	}
}

// The same files pack to the same bytes, so an unchanged folder uploads as
// nothing new.
func TestPackIsTheSameForTheSameFiles(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"a.txt": "a", "b/c.txt": "c"})
	first, err := PackFolder(dir)
	if err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(time.Hour)
	os.Chtimes(filepath.Join(dir, "a.txt"), later, later)
	second, err := PackFolder(dir)
	if err != nil {
		t.Fatal(err)
	}
	if first.Digest != second.Digest {
		t.Fatal("same files, different archive")
	}
}

// A link out of the folder would build something other than what is there.
func TestPackRefusesALinkOutOfTheFolder(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"a.txt": "a"})
	if err := os.Symlink("/etc/passwd", filepath.Join(dir, "passwd")); err != nil {
		t.Skip(err)
	}
	if _, err := PackFolder(dir); err == nil {
		t.Fatal("packed a link out of the folder")
	}
}

func TestPackRefusesAFolderWithNothingLeft(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{".env": "X=1", "node_modules/a.js": "a"})
	if _, err := PackFolder(dir); err == nil {
		t.Fatal("packed nothing")
	}
}

func TestIgnorePatterns(t *testing.T) {
	cases := []struct {
		pattern, path string
		dir, want     bool
	}{
		{"*.log", "a/b/x.log", false, true},
		{"/build", "build", true, true},
		{"/build", "src/build", true, false},
		{"build/", "src/build", true, true},
		{"build/", "build", false, false},
		{"docs/**/*.png", "docs/a/b/c.png", false, true},
		{"**/tmp", "x/y/tmp", true, true},
		{"a?c", "abc", false, true},
	}
	for _, c := range cases {
		r, _ := parseIgnoreLine(c.pattern)
		if got := (ignoreRules{r}).match(c.path, c.dir); got != c.want {
			t.Errorf("%q on %q: %v, want %v", c.pattern, c.path, got, c.want)
		}
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

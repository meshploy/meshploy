package client

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// MaxPackedBytes is the most the API takes for a folder, packed.
const MaxPackedBytes = 100 << 20

// PackedFolder is a folder packed as the API takes it: a tar.gz of what is
// left once the ignore rules have been applied.
type PackedFolder struct {
	Name    string // the folder's own name
	Archive []byte
	Digest  string // sha256:<hex> of Archive, as the API will record it
	Files   int
	// Skipped is what was left out by a rule rather than by an ignore file:
	// the folders and files a user may wonder about.
	Skipped []string
}

// ErrPackTooLarge is a folder over MaxPackedBytes once packed.
var ErrPackTooLarge = fmt.Errorf("the folder is over %d MB packed: add what the build does not need to .meshployignore", MaxPackedBytes>>20)

// alwaysSkipped are never sent: history, installed dependencies (the build
// installs its own), and anything that holds secrets.
var alwaysSkipped = []string{".git", "node_modules", ".venv", "venv", "__pycache__", ".DS_Store"}

// isEnvFile is a .env file holding real values; an example of one is kept.
func isEnvFile(name string) bool {
	if name != ".env" && !strings.HasPrefix(name, ".env.") {
		return false
	}
	for _, keep := range []string{".example", ".sample", ".template"} {
		if strings.HasSuffix(name, keep) {
			return false
		}
	}
	return true
}

// PackFolder packs dir for upload. It leaves out .git, installed dependencies
// and .env files always, and what the folder's .gitignore and .meshployignore
// name. The archive is the same bytes for the same files, so uploading an
// unchanged folder again stores nothing new.
func PackFolder(dir string) (*PackedFolder, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a folder", dir)
	}
	ignore := loadIgnore(abs, ".gitignore", ".meshployignore")

	type entry struct {
		rel  string
		info fs.FileInfo
	}
	var entries []entry
	var skipped []string
	err = filepath.WalkDir(abs, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == abs {
			return nil
		}
		rel := filepath.ToSlash(strings.TrimPrefix(p, abs+string(filepath.Separator)))
		name := d.Name()
		if isEnvFile(name) || containsString(alwaysSkipped, name) {
			skipped = append(skipped, rel)
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if ignore.match(rel, d.IsDir()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		// Sockets, devices and the like are not part of an app.
		if !fi.Mode().IsRegular() && !fi.IsDir() && fi.Mode()&fs.ModeSymlink == 0 {
			return nil
		}
		entries = append(entries, entry{rel, fi})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].rel < entries[j].rel })

	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	files := 0
	epoch := time.Unix(0, 0)
	for _, e := range entries {
		hdr := &tar.Header{Name: e.rel, ModTime: epoch, Format: tar.FormatPAX}
		switch {
		case e.info.IsDir():
			hdr.Typeflag, hdr.Name, hdr.Mode = tar.TypeDir, e.rel+"/", 0o755
		case e.info.Mode()&fs.ModeSymlink != 0:
			target, err := os.Readlink(filepath.Join(abs, filepath.FromSlash(e.rel)))
			if err != nil {
				return nil, err
			}
			// A link out of the folder would not be there to build; the API
			// refuses the archive rather than build something different.
			resolved := path.Join(path.Dir(e.rel), filepath.ToSlash(target))
			if filepath.IsAbs(target) || resolved == ".." || strings.HasPrefix(resolved, "../") {
				return nil, fmt.Errorf("%s links outside the folder (%s): replace it with what it points to, or ignore it", e.rel, target)
			}
			hdr.Typeflag, hdr.Linkname, hdr.Mode = tar.TypeSymlink, filepath.ToSlash(target), 0o777
		default:
			hdr.Typeflag, hdr.Size, hdr.Mode = tar.TypeReg, e.info.Size(), 0o644
			if e.info.Mode()&0o111 != 0 {
				hdr.Mode = 0o755
			}
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return nil, err
		}
		if hdr.Typeflag == tar.TypeReg {
			body, err := os.ReadFile(filepath.Join(abs, filepath.FromSlash(e.rel)))
			if err != nil {
				return nil, err
			}
			if _, err := tw.Write(body); err != nil {
				return nil, err
			}
			files++
		}
		if buf.Len() > MaxPackedBytes {
			return nil, ErrPackTooLarge
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	if buf.Len() > MaxPackedBytes {
		return nil, ErrPackTooLarge
	}
	if files == 0 {
		return nil, errors.New("nothing to upload: every file in the folder is ignored")
	}
	return &PackedFolder{
		Name:    filepath.Base(abs),
		Archive: buf.Bytes(),
		Digest:  fmt.Sprintf("sha256:%x", sha256.Sum256(buf.Bytes())),
		Files:   files,
		Skipped: skipped,
	}, nil
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// ── Ignore files ─────────────────────────────────────────────────────────────

// ignoreRules are the patterns of a folder's top-level ignore files, in the
// .gitignore syntax: # comments, ! to take a match back, a trailing / for a
// folder only, a leading or inner / anchoring to the folder, and *, ? and **.
// Nested .gitignore files are not read.
type ignoreRules []ignoreRule

type ignoreRule struct {
	re      *regexp.Regexp
	negate  bool
	dirOnly bool
}

func loadIgnore(dir string, names ...string) ignoreRules {
	var rules ignoreRules
	for _, name := range names {
		f, err := os.Open(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			if r, ok := parseIgnoreLine(sc.Text()); ok {
				rules = append(rules, r)
			}
		}
		f.Close()
	}
	return rules
}

func parseIgnoreLine(line string) (ignoreRule, bool) {
	line = strings.TrimRight(line, " \t\r")
	if line == "" || strings.HasPrefix(line, "#") {
		return ignoreRule{}, false
	}
	var r ignoreRule
	if strings.HasPrefix(line, "!") {
		r.negate, line = true, line[1:]
	}
	if strings.HasSuffix(line, "/") {
		r.dirOnly, line = true, strings.TrimSuffix(line, "/")
	}
	anchored := strings.Contains(line, "/")
	line = strings.TrimPrefix(line, "/")
	if line == "" {
		return ignoreRule{}, false
	}
	var b strings.Builder
	if !anchored {
		b.WriteString("(^|.*/)")
	} else {
		b.WriteString("^")
	}
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case c == '*' && i+1 < len(line) && line[i+1] == '*':
			i++
			if i+1 < len(line) && line[i+1] == '/' {
				i++
				b.WriteString("(.*/)?")
			} else {
				b.WriteString(".*")
			}
		case c == '*':
			b.WriteString("[^/]*")
		case c == '?':
			b.WriteString("[^/]")
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	// A folder's match covers everything in it.
	b.WriteString("(/.*)?$")
	re, err := regexp.Compile(b.String())
	if err != nil {
		return ignoreRule{}, false
	}
	r.re = re
	return r, true
}

// match says whether rel (slash-separated, relative to the folder) is
// ignored: the last rule that matches decides.
func (rules ignoreRules) match(rel string, isDir bool) bool {
	ignored := false
	for _, r := range rules {
		if r.dirOnly && !isDir && !r.re.MatchString(path.Dir(rel)) {
			continue
		}
		if r.re.MatchString(rel) {
			ignored = !r.negate
		}
	}
	return ignored
}

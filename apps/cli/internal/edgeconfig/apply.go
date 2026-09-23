package edgeconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// BackupDirName holds what the last apply replaced, inside the install
// directory so uninstalling takes it with the rest. One generation deep: it
// exists so a configuration that does not come up can be put straight back,
// not as a history.
//
// Deliberately not the scheme this replaces. Caddyfile.delegation.bak was a
// copy of a template kept indefinitely, and it went stale: switching back to
// delegation restored the first install's file over a newer release's. A
// backup of an output, for exactly one step, cannot go stale because nothing
// reads it except a rollback that happens seconds later.
const BackupDirName = ".edge-previous"

// StagedCaddyfile is where a rendered Caddyfile waits while Caddy is asked
// whether it would accept it. Beside the real one, because `import
// conf.d/*.caddy` resolves relative to the config file and validating anywhere
// else would not see what the operator added.
const StagedCaddyfile = "caddy/Caddyfile.staged"

// generatedMarker is in the header of every file this package writes. It is how
// a stale zone is told apart from a file somebody put there: only a file
// carrying it is ever removed.
const generatedMarker = "generated, do not edit"

// zonesDir is the one directory whose contents are owned by the render. A
// domain that is removed, or switched to a mode that no longer needs a zone,
// leaves a file here that nothing serves; left alone they accumulate, and the
// next person to read the directory cannot tell which are live.
const zonesDir = "coredns/zones"

// challengeZonePrefix names the zones Caddy writes into.
//
// A DNS-01 challenge is answered by the `dns meshploy` module writing a TXT
// record straight into the zone file and removing it once the certificate is
// issued - that is what zone_file_path in the Caddyfile points at, and why
// docker-compose mounts coredns/zones into Caddy writable. So these files are
// live state, not configuration: rendering one is seeding it, and rewriting it
// later would delete a challenge that is in flight. It would also mean an apply
// never finding nothing to do, because any certificate being issued at that
// moment is a difference.
const challengeZonePrefix = zonesDir + "/_acme-challenge."

// seedOnly reports whether a path is written once and then left alone.
func seedOnly(path string) bool { return strings.HasPrefix(path, challengeZonePrefix) }

// A Change is one file an apply would write, or remove.
type Change struct {
	Path   string
	Action string
	Before string
	After  string
}

// Actions a Change can carry.
const (
	ActionCreate = "create"
	ActionUpdate = "update"
	ActionRemove = "remove"
)

func (c Change) String() string { return c.Action + " " + c.Path }

// Plan compares a render with what is on disk, and returns only what differs.
//
// The comparison is the point. Rendering is driven by a snapshot that is
// rewritten whenever anything about a domain changes, including changes that do
// not reach the edge at all, and applying unconditionally would reload Caddy
// and restart CoreDNS every time. An apply that finds nothing to do must do
// nothing.
func Plan(files Files, dir string) ([]Change, error) {
	var changes []Change
	for _, p := range files.Paths() {
		before, err := os.ReadFile(filepath.Join(dir, p))
		switch {
		case os.IsNotExist(err):
			changes = append(changes, Change{Path: p, Action: ActionCreate, After: files[p]})
			continue
		case err != nil:
			return nil, err
		}
		if seedOnly(p) {
			continue
		}
		if string(before) != files[p] {
			changes = append(changes, Change{Path: p, Action: ActionUpdate, Before: string(before), After: files[p]})
		}
	}
	stale, err := staleZones(files, dir)
	if err != nil {
		return nil, err
	}
	changes = append(changes, stale...)
	sort.Slice(changes, func(i, j int) bool { return changes[i].Path < changes[j].Path })
	return changes, nil
}

// staleZones finds generated zone files the render no longer produces.
//
// Only a file carrying the generated header is a candidate. A zone somebody
// wrote by hand, and the {DOMAIN}-named templates an older install left in this
// directory, are left exactly where they are: this decides what is stale, and
// it must never decide that about a file it did not write.
func staleZones(files Files, dir string) ([]Change, error) {
	entries, err := os.ReadDir(filepath.Join(dir, zonesDir))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Change
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		rel := zonesDir + "/" + e.Name()
		if _, rendered := files[rel]; rendered {
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, rel))
		if err != nil {
			return nil, err
		}
		if !strings.Contains(string(body), generatedMarker) {
			continue
		}
		out = append(out, Change{Path: rel, Action: ActionRemove, Before: string(body)})
	}
	return out, nil
}

// Install writes the changes, saving what each one replaces under
// BackupDirName first. Nothing else in the install directory is touched.
func Install(changes []Change, dir string) error {
	backup := filepath.Join(dir, BackupDirName)
	if err := os.RemoveAll(backup); err != nil {
		return err
	}
	if err := os.MkdirAll(backup, 0o700); err != nil {
		return err
	}
	// A manifest, so a restore knows a create has to be undone by removing the
	// file rather than by writing an empty one back.
	var created []string
	for _, c := range changes {
		if c.Action == ActionCreate {
			created = append(created, c.Path)
			continue
		}
		dst := filepath.Join(backup, filepath.FromSlash(c.Path))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(dst, []byte(c.Before), 0o644); err != nil {
			return err
		}
	}
	if len(created) > 0 {
		list := strings.Join(created, "\n") + "\n"
		if err := os.WriteFile(filepath.Join(backup, "created"), []byte(list), 0o644); err != nil {
			return err
		}
	}

	for _, c := range changes {
		target := filepath.Join(dir, filepath.FromSlash(c.Path))
		if c.Action == ActionRemove {
			if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := writeAtomic(target, c.After); err != nil {
			return err
		}
	}
	return nil
}

// Restore puts back what the last Install replaced. Used when the configuration
// it wrote does not come up: a gateway that cannot serve is one nobody can
// reach to fix, including whoever ran the apply.
func Restore(dir string) error {
	backup := filepath.Join(dir, BackupDirName)
	if _, err := os.Stat(backup); err != nil {
		return fmt.Errorf("nothing to restore: %w", err)
	}
	// Files that did not exist before are removed rather than written back.
	if list, err := os.ReadFile(filepath.Join(backup, "created")); err == nil {
		for _, p := range strings.Split(strings.TrimSpace(string(list)), "\n") {
			if p == "" {
				continue
			}
			if err := os.Remove(filepath.Join(dir, filepath.FromSlash(p))); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
	}
	return filepath.Walk(backup, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, err := filepath.Rel(backup, p)
		if err != nil {
			return err
		}
		if rel == "created" {
			return nil
		}
		body, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		target := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return writeAtomic(target, string(body))
	})
}

// writeAtomic replaces a file through a rename, so a reader never sees half of
// one. CoreDNS re-reads its zones on a timer and Caddy is reloaded while it is
// serving, so both can read a file at the moment it is being written.
func writeAtomic(path, content string) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

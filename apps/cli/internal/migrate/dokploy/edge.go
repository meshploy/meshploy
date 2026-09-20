package dokploy

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/meshploy/apps/cli/internal/migrate/journal"
)

// Applying a domain switch to Dokploy's Traefik, on disk.
//
// Traefik's file provider watches the dynamic directory, so a written file
// takes effect within seconds and no restart is needed - which is the whole
// reason a domain can move before cutover.
//
// Every write is backed up into the journal's own directory first. The
// migration never edits a file it has not saved a copy of.

// DefaultDynamicDir is where Dokploy's Traefik reads dynamic configuration,
// taken from its static config's file provider.
const DefaultDynamicDir = "/etc/dokploy/traefik/dynamic"

// DefaultTraefikDir is Dokploy's Traefik configuration, backed up whole before
// cutover.
const DefaultTraefikDir = "/etc/dokploy/traefik"

// EdgeSwitcher points Dokploy's domains at Meshploy, one app at a time.
type EdgeSwitcher struct {
	// DynamicDir is Traefik's watched directory.
	DynamicDir string
	// Target is where switched domains go: Meshploy's proxy, as seen from
	// inside Traefik's container. See ProxyTarget.
	Target  string
	Journal *journal.Journal
}

// SwitchApp rewrites one application's dynamic file so its hostnames reach
// Meshploy, and records the original for rollback.
//
// For an application, whose router Dokploy wrote to a file.
func (e EdgeSwitcher) SwitchApp(step, group, appName string) error {
	if e.Journal != nil && e.Journal.Done(step) {
		return nil
	}
	entry := journal.Entry{Step: step, Group: group, Action: "switch-domain", Target: appName, Result: journal.OK}

	path := filepath.Join(e.dir(), appName+".yml")
	original, err := os.ReadFile(path)
	if err != nil {
		return e.fail(entry, fmt.Errorf("read %s: %w", path, err))
	}
	switched, _, err := SwitchServiceURL(original, e.Target)
	if err != nil {
		return e.fail(entry, fmt.Errorf("%s: %w", path, err))
	}
	backup, err := e.Journal.Backup(appName+".yml", original)
	if err != nil {
		return e.fail(entry, fmt.Errorf("back up %s: %w", path, err))
	}
	// Written whole and renamed into place: Traefik is watching, and it must
	// never read half a file.
	if err := writeAtomic(path, switched); err != nil {
		return e.fail(entry, err)
	}
	entry.Undo = &journal.Undo{Kind: journal.UndoRestoreFile, Args: map[string]string{
		"path": path, "backup": backup,
	}}
	return e.append(entry)
}

// TakeOverHosts writes a higher-priority router for hostnames Traefik learned
// from labels, which is how a compose app's domains move.
func (e EdgeSwitcher) TakeOverHosts(step, group, name string, hosts []string) error {
	if e.Journal != nil && e.Journal.Done(step) {
		return nil
	}
	entry := journal.Entry{Step: step, Group: group, Action: "take-over-domain", Target: name, Result: journal.OK}

	content, err := OverrideFile(name, hosts, e.Target)
	if err != nil {
		return e.fail(entry, err)
	}
	path := filepath.Join(e.dir(), OverrideFileName(name))
	// Refusing to overwrite something that is not ours: a name collision with a
	// Dokploy file would take an app off the air with no record of what was
	// there.
	if existing, err := os.ReadFile(path); err == nil && !isOurs(existing) {
		return e.fail(entry, fmt.Errorf("%s exists and was not written by Meshploy", path))
	}
	if err := writeAtomic(path, content); err != nil {
		return e.fail(entry, err)
	}
	entry.Undo = &journal.Undo{Kind: journal.UndoRemoveFile, Args: map[string]string{"path": path}}
	return e.append(entry)
}

func (e EdgeSwitcher) dir() string {
	if e.DynamicDir == "" {
		return DefaultDynamicDir
	}
	return e.DynamicDir
}

func (e EdgeSwitcher) append(en journal.Entry) error {
	if e.Journal == nil {
		return nil
	}
	return e.Journal.Append(en)
}

func (e EdgeSwitcher) fail(en journal.Entry, cause error) error {
	en.Result, en.Error = journal.Failed, cause.Error()
	_ = e.append(en)
	return cause
}

// isOurs reports whether a dynamic file was written by this migration, by the
// header OverrideFile puts at the top.
func isOurs(content []byte) bool {
	return len(content) > 21 && string(content[:21]) == "# Written by Meshploy"
}

// writeAtomic writes beside the target and renames, so a watcher never sees a
// partial file.
func writeAtomic(path string, content []byte) error {
	tmp := path + ".meshploy-tmp"
	if err := os.WriteFile(tmp, content, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}

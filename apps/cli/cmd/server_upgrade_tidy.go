package cmd

import (
	"fmt"
	"os"
	"path/filepath"
)

// An upgrade leaves the images it replaced behind, untagged, and journald
// keeps a tenth of the disk: on a small gateway the two fill it. Tidying runs
// after an upgrade has succeeded and fails nothing, since the server is
// already right by then. A rollback needs only the images running when an
// upgrade starts, which are in use and so are never pruned.

// journalCap is the drop-in that holds a gateway's journal to a size. One
// already there is the operator's, and stays.
var journalCap = "/etc/systemd/journald.conf.d/50-meshploy.conf"

const journalCapBody = "# Written by Meshploy: a gateway's disk is for what it runs.\n[Journal]\nSystemMaxUse=500M\n"

func tidyAfterUpgrade(runtime string) {
	fmt.Println("Removing the images this upgrade replaced…")
	if err := runtimeExec(runtime, "image", "prune", "-f"); err != nil {
		fmt.Printf("warning: could not remove old images: %v\n", err)
	}
	if err := capJournal(); err != nil {
		fmt.Printf("warning: could not cap the system journal: %v\n", err)
	}
}

func capJournal() error {
	if _, err := os.Stat(journalCap); err == nil {
		return nil
	}
	// No systemd, nothing to cap.
	if _, err := os.Stat(filepath.Dir(filepath.Dir(journalCap))); err != nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(journalCap), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(journalCap, []byte(journalCapBody), 0o644); err != nil {
		return err
	}
	return systemctlRestart("systemd-journald")
}

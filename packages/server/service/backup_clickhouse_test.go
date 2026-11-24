package service

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	db "github.com/meshploy/packages/db"
)

// A ClickHouse backup is the server's own BACKUP, kept under a fixed name so
// it restores into any database, and a restore proves the file in a scratch
// database before it drops the live one.
func TestClickHouseBackupCommands(t *testing.T) {
	dump, err := dumpCmd(db.DatabaseClickHouse, "u", "app", "p'w")
	if err != nil {
		t.Fatal(err)
	}
	d := dump[2]
	for _, want := range []string{
		"BACKUP DATABASE `app` AS backup TO File(",
		`--password 'p'"'"'w'`,
		"rm -f '/var/lib/clickhouse/backups/meshploy-",
		"exit $rc",
	} {
		if !strings.Contains(d, want) {
			t.Errorf("dump command lacks %q:\n%s", want, d)
		}
	}

	restore, err := restoreCmd(db.DatabaseClickHouse, "u", "app", "p")
	if err != nil {
		t.Fatal(err)
	}
	r := restore[2]
	scratch := strings.Index(r, "RESTORE DATABASE backup AS `app_meshploy_restore`")
	drop := strings.Index(r, "DROP DATABASE IF EXISTS `app` SYNC")
	live := strings.Index(r, "RESTORE DATABASE backup AS `app` FROM")
	if scratch < 0 || drop < 0 || live < 0 || !(scratch < drop && drop < live) {
		t.Errorf("restore must prove the file in a scratch database, then drop and restore the live one:\n%s", r)
	}
}

func TestChIdentQuotes(t *testing.T) {
	if got := chIdent("a`b\\c"); got != "`a\\`b\\\\c`" {
		t.Errorf("chIdent = %s", got)
	}
}

// TestClickHouseBackupRoundTrip runs the commands against a real server:
// MESHPLOY_CLICKHOUSE_CONTAINER names a running clickhouse-server container
// (podman or docker, MESHPLOY_CONTAINER_CLI) whose user is u, password p and
// database app holding a table t.
func TestClickHouseBackupRoundTrip(t *testing.T) {
	ctr := os.Getenv("MESHPLOY_CLICKHOUSE_CONTAINER")
	if ctr == "" {
		t.Skip("MESHPLOY_CLICKHOUSE_CONTAINER not set")
	}
	cli := os.Getenv("MESHPLOY_CONTAINER_CLI")
	if cli == "" {
		cli = "podman"
	}
	run := func(cmd []string, stdin []byte) []byte {
		t.Helper()
		args := append([]string{"exec", "-i", ctr}, cmd...)
		c := exec.Command(cli, args...)
		if stdin != nil {
			c.Stdin = strings.NewReader(string(stdin))
		}
		out, err := c.Output()
		if err != nil {
			t.Fatalf("%v: %v", cmd, err)
		}
		return out
	}
	count := func() string {
		return strings.TrimSpace(string(run([]string{"clickhouse-client", "-u", "u", "--password", "p", "-q", "SELECT count() FROM app.t"}, []byte{})))
	}

	before := count()
	dump, _ := dumpCmd(db.DatabaseClickHouse, "u", "app", "p")
	zip := run(dump, nil)
	if len(zip) < 4 || string(zip[:2]) != "PK" {
		t.Fatalf("backup is not a zip (%d bytes)", len(zip))
	}
	run([]string{"clickhouse-client", "-u", "u", "--password", "p", "-q", "TRUNCATE TABLE app.t"}, []byte{})
	restore, _ := restoreCmd(db.DatabaseClickHouse, "u", "app", "p")
	run(restore, zip)
	if after := count(); after != before {
		t.Errorf("restored %s rows, backed up %s", after, before)
	}
	left := run([]string{"sh", "-c", "ls /var/lib/clickhouse/backups | grep -c meshploy- || true"}, nil)
	if strings.TrimSpace(string(left)) != "0" {
		t.Errorf("backup files left behind: %s", left)
	}

	// A file that is not a backup fails before the live database is dropped.
	c := exec.Command(cli, append([]string{"exec", "-i", ctr}, restore...)...)
	c.Stdin = strings.NewReader("not a zip")
	if c.Run() == nil {
		t.Error("restoring garbage succeeded")
	}
	if after := count(); after != before {
		t.Errorf("a failed restore changed the live database: %s rows, had %s", after, before)
	}
}

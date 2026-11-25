package service

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	db "github.com/meshploy/packages/db"
)

// A Redis or Dragonfly backup is sent only when it is a snapshot: a failed
// run used to send its error text as the backup and count as a success.
func TestRedisBackupSendsOnlySnapshots(t *testing.T) {
	for _, engine := range []db.DatabaseEngine{db.DatabaseRedis, db.DatabaseDragonfly} {
		cmd, err := dumpCmd(engine, "", "", "p'w")
		if err != nil {
			t.Fatal(err)
		}
		c := cmd[2]
		for _, want := range []string{
			`export REDISCLI_AUTH='p'"'"'w'`,
			`= REDIS ] && gzip -c`,
			"exit $rc",
		} {
			if !strings.Contains(c, want) {
				t.Errorf("%s backup lacks %q:\n%s", engine, want, c)
			}
		}
		if strings.Contains(c, "| gzip") {
			t.Errorf("%s backup pipes into gzip, which hides a failed snapshot:\n%s", engine, c)
		}
		if strings.Contains(c, "-a ") {
			t.Errorf("%s backup puts the password on the command line:\n%s", engine, c)
		}
	}
	df, _ := dumpCmd(db.DatabaseDragonfly, "", "", "")
	if !strings.Contains(df[2], "save rdb") || strings.Contains(df[2], "--rdb") {
		t.Errorf("Dragonfly has no SYNC, so it must save its own snapshot:\n%s", df[2])
	}
}

func TestRedisRestoreStopsWithoutSaving(t *testing.T) {
	for _, engine := range []db.DatabaseEngine{db.DatabaseRedis, db.DatabaseDragonfly} {
		cmd, err := restoreCmd(engine, "", "", "")
		if err != nil {
			t.Fatal(err)
		}
		c := cmd[2]
		if !strings.Contains(c, "shutdown nosave") || !strings.HasSuffix(c, "&") {
			t.Errorf("%s restore must stop the server without saving, after the exec ends:\n%s", engine, c)
		}
		if strings.Contains(c, "export REDISCLI_AUTH") {
			t.Errorf("%s restore signs in without a password:\n%s", engine, c)
		}
	}
}

// TestRedisBackupRoundTrip runs backup and restore against real servers:
// MESHPLOY_REDIS_CONTAINER and MESHPLOY_DRAGONFLY_CONTAINER name running
// containers (podman or docker, MESHPLOY_CONTAINER_CLI) with password p, a
// restart policy that restarts them, and their data in /data.
func TestRedisBackupRoundTrip(t *testing.T) {
	cli := os.Getenv("MESHPLOY_CONTAINER_CLI")
	if cli == "" {
		cli = "podman"
	}
	cases := map[db.DatabaseEngine]string{
		db.DatabaseRedis:     os.Getenv("MESHPLOY_REDIS_CONTAINER"),
		db.DatabaseDragonfly: os.Getenv("MESHPLOY_DRAGONFLY_CONTAINER"),
	}
	ran := false
	for engine, ctr := range cases {
		if ctr == "" {
			continue
		}
		ran = true
		t.Run(string(engine), func(t *testing.T) {
			execIn := func(cmd []string, stdin string) (string, error) {
				c := exec.Command(cli, append([]string{"exec", "-i", ctr}, cmd...)...)
				c.Stdin = strings.NewReader(stdin)
				out, err := c.Output()
				return string(out), err
			}
			redis := func(args ...string) string {
				out, err := execIn(append([]string{"redis-cli", "--no-auth-warning", "-a", "p"}, args...), "")
				if err != nil {
					t.Fatalf("redis-cli %v: %v", args, err)
				}
				return strings.TrimSpace(out)
			}
			awaitBack := func() {
				for i := 0; i < 60; i++ {
					time.Sleep(time.Second)
					if out, err := execIn([]string{"redis-cli", "--no-auth-warning", "-a", "p", "ping"}, ""); err == nil && strings.TrimSpace(out) == "PONG" {
						return
					}
				}
				t.Fatal("server did not come back")
			}

			redis("flushall")
			redis("set", "kept", "yes")
			redis("rpush", "queue", "a", "b", "c")
			dump, _ := dumpCmd(engine, "", "", "p")
			snap, err := execIn(dump, "")
			if err != nil {
				t.Fatalf("backup: %v", err)
			}

			redis("set", "later", "drop-me")
			redis("del", "kept")
			restore, _ := restoreCmd(engine, "", "", "p")
			if _, err := execIn(restore, snap); err != nil {
				t.Fatalf("restore: %v", err)
			}
			time.Sleep(3 * time.Second)
			awaitBack()
			if got := redis("get", "kept"); got != "yes" {
				t.Errorf("kept = %q after restore", got)
			}
			if got := redis("llen", "queue"); got != "3" {
				t.Errorf("queue has %s items after restore", got)
			}
			if got := redis("exists", "later"); got != "0" {
				t.Errorf("a key written after the backup survived the restore")
			}

			// Something that is not a snapshot is refused, and the server
			// keeps running with its data.
			if _, err := execIn(restore, "not a snapshot"); err == nil {
				t.Error("restoring garbage succeeded")
			}
			time.Sleep(2 * time.Second)
			if got := redis("get", "kept"); got != "yes" {
				t.Errorf("a refused restore changed the data: kept = %q", got)
			}
		})
	}
	if !ran {
		t.Skip("MESHPLOY_REDIS_CONTAINER and MESHPLOY_DRAGONFLY_CONTAINER not set")
	}
}

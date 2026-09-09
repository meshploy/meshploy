package setup

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeScript writes a stand-in for install.sh so the runner can be exercised
// without provisioning a machine.
func fakeScript(t *testing.T, body string) ScriptRunner {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "install.sh")
	if err := os.WriteFile(path, []byte("#!/usr/bin/env bash\n"+body), 0755); err != nil {
		t.Fatal(err)
	}
	return ScriptRunner{Script: path, Dir: dir}
}

func collect(t *testing.T, r ScriptRunner, a Answers) ([]string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var lines []string
	err := r.Run(ctx, a, func(l string) { lines = append(lines, l) })
	return lines, err
}

var validAnswers = Answers{
	Domain: "example.com", DNSMode: "ondemand", PublicIP: "203.0.113.10", MeshIP: "100.64.0.1",
}

// The answers must arrive as environment variables, because that is how
// install.sh's `ask` reads them under --auto. Getting this wrong means the
// installer stops and asks a question nobody can answer.
func TestAnswersReachTheScriptAsEnvironment(t *testing.T) {
	r := fakeScript(t, `
echo "NODE_TYPE=$NODE_TYPE"
echo "DOMAIN=$DOMAIN"
echo "PUBLIC_IP=$PUBLIC_IP"
echo "MESH_IP=$MESH_IP"
echo "ARGS=$*"
`)
	lines, err := collect(t, r, validAnswers)
	if err != nil {
		t.Fatalf("run: %v (%v)", err, lines)
	}
	got := strings.Join(lines, "\n")

	for _, want := range []string{
		"NODE_TYPE=master",
		"DOMAIN=example.com",
		"PUBLIC_IP=203.0.113.10",
		"MESH_IP=100.64.0.1",
		"ARGS=--auto --dns-mode=ondemand",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

// A password typed into the wizard would cross a plaintext connection. Leaving
// it unset makes install.sh generate one instead, which is strictly better.
func TestPostgresPasswordIsNotPassedThrough(t *testing.T) {
	r := fakeScript(t, `echo "PG=[${POSTGRES_PASSWORD:-unset}]"`)
	lines, err := collect(t, r, validAnswers)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(lines, "\n"), "PG=[unset]") {
		t.Errorf("the installer should generate its own password, got %v", lines)
	}
}

// stderr belongs in the same transcript as stdout, in the order it happened —
// the operator is reading one log, not correlating two.
func TestStderrIsInterleavedIntoTheTranscript(t *testing.T) {
	r := fakeScript(t, `
echo "first"
echo "a warning" >&2
echo "last"
`)
	lines, err := collect(t, r, validAnswers)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(lines, "\n")
	for _, want := range []string{"first", "a warning", "last"} {
		if !strings.Contains(got, want) {
			t.Errorf("transcript missing %q:\n%s", want, got)
		}
	}
}

// install.sh is heavily coloured. Escape codes rendered in a browser sit in
// front of the text the operator is trying to read.
func TestColourCodesAreStripped(t *testing.T) {
	r := fakeScript(t, `printf '\033[0;32m  ok\033[0m  images pulled\n'`)
	lines, err := collect(t, r, validAnswers)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(lines, "\n")
	if strings.Contains(got, "\x1b[") {
		t.Errorf("escape codes survived: %q", got)
	}
	if !strings.Contains(got, "images pulled") {
		t.Errorf("the text itself was lost: %q", got)
	}
}

// A failed install must surface as an error the wizard can show, with the
// transcript already delivered so the operator can see what happened.
func TestFailureReportsTheExitStatusAndKeepsTheLog(t *testing.T) {
	r := fakeScript(t, `
echo "pulling images"
echo "something broke" >&2
exit 3
`)
	lines, err := collect(t, r, validAnswers)
	if err == nil {
		t.Fatal("a non-zero exit must be an error")
	}
	if !strings.Contains(err.Error(), "status 3") {
		t.Errorf("the exit status should be named, got: %v", err)
	}
	if !strings.Contains(strings.Join(lines, "\n"), "something broke") {
		t.Errorf("output before the failure must still reach the browser, got %v", lines)
	}
}

// Closing the browser cancels the request. The installer must not be left
// running unattended against a half-configured box.
func TestCancellingTheContextStopsTheInstaller(t *testing.T) {
	r := fakeScript(t, `echo started; sleep 60; echo "should not get here"`)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- r.Run(ctx, validAnswers, func(l string) {
			if strings.Contains(l, "started") {
				cancel()
			}
		})
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Error("a cancelled install should report an error, not success")
		}
	case <-time.After(20 * time.Second):
		t.Fatal("the installer kept running after the context was cancelled")
	}
}

package setup

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// End to end through the real Store, the real ScriptRunner and the real HTTP
// surface — everything except the cobra command and its root check. The only
// stand-in is install.sh itself, because the real one provisions a machine.
func TestFullSetupFlow(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "install.sh")
	if err := os.WriteFile(script, []byte(`#!/usr/bin/env bash
printf '\033[0;36m  →\033[0m  Auto: Base domain → %s\n' "$DOMAIN"
echo "  ok  node type: $NODE_TYPE"
echo "  ok  args: $*"
echo "  ok  core services started"
`), 0755); err != nil {
		t.Fatal(err)
	}

	store, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	res := fakeResolver{hosts: map[string][]string{
		"console.example.com": {"203.0.113.10"},
		"api.example.com":     {"203.0.113.10"},
	}}
	srv := httptest.NewServer(NewServer(store, "ms_e2e", res,
		ScriptRunner{Script: script, Dir: dir}).Handler())
	defer srv.Close()

	h := srv.Config.Handler

	// 1. The page renders before anything is authenticated — it has to, in order
	//    to ask for the token.
	if got := do(h, "GET", "/", "", "").Code; got != 200 {
		t.Fatalf("setup page: want 200, got %d", got)
	}

	// 2. Check a domain without committing it.
	if got := do(h, "POST", "/api/check-domain", "ms_e2e",
		`{"domain":"example.com","dns_mode":"ondemand","public_ip":"203.0.113.10"}`).Code; got != 200 {
		t.Fatalf("check-domain: got %d", got)
	}

	// 3. Save the answers.
	if got := do(h, "POST", "/api/answers", "ms_e2e",
		`{"domain":"example.com","dns_mode":"ondemand","public_ip":"203.0.113.10","mesh_ip":"100.64.0.1"}`).Code; got != 200 {
		t.Fatalf("answers: got %d", got)
	}

	// 4. Run it, and read the stream.
	w := do(h, "POST", "/api/install", "ms_e2e", "")
	body := w.Body.String()

	for _, want := range []string{
		"Auto: Base domain → example.com", // the answer reached the script
		"node type: master",               // NODE_TYPE was set for --auto
		"--auto --dns-mode=ondemand",      // the flag was forwarded
		"core services started",
		"event: done",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("stream missing %q\n---\n%s", want, body)
		}
	}
	if strings.Contains(body, "\x1b[") {
		t.Error("terminal colour codes reached the browser")
	}

	// 5. State advanced and the transcript persisted.
	st := store.Get()
	if st.Phase != PhaseVerify {
		t.Errorf("want phase %q, got %q", PhaseVerify, st.Phase)
	}
	if len(st.Log) == 0 {
		t.Error("the transcript should have been persisted")
	}

	// 6. A fresh process — the operator reloaded — resumes rather than restarts.
	reopened, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := reopened.Get()
	if got.Phase != PhaseVerify || got.Answers.Domain != "example.com" {
		t.Errorf("a reload lost the install: %+v", got)
	}
	if len(got.Log) != len(st.Log) {
		t.Errorf("transcript not replayable: had %d lines, reloaded %d", len(st.Log), len(got.Log))
	}
}

// The page is the only unauthenticated route, and it must carry nothing that
// would let a passer-by act — it is served over plain HTTP on a public IP.
func TestPageLeaksNothing(t *testing.T) {
	store, _ := NewStore(t.TempDir())
	_ = store.Update(func(st *State) {
		st.Answers = Answers{Domain: "secret-internal.example.com", PublicIP: "203.0.113.10"}
	})
	h := NewServer(store, "ms_supersecret", fakeResolver{}, &fakeRunner{}).Handler()

	body := do(h, "GET", "/", "", "").Body.String()
	for _, leak := range []string{"ms_supersecret", "secret-internal.example.com", "203.0.113.10"} {
		if strings.Contains(body, leak) {
			t.Errorf("the setup page leaks %q to an unauthenticated visitor", leak)
		}
	}
}

// The install keeps running when a browser disconnects, so reattaching must
// show the transcript rather than an empty pane.
func TestTranscriptSurvivesADisconnect(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(dir)
	_ = store.Update(func(st *State) {
		st.Answers = Answers{Domain: "example.com", DNSMode: "ondemand", PublicIP: "203.0.113.10"}
	})
	h := NewServer(store, "ms_e2e", fakeResolver{},
		&fakeRunner{lines: []string{"line one", "line two"}}).Handler()

	do(h, "POST", "/api/install", "ms_e2e", "")

	// Reattach: GET /api/state is what the page calls on load.
	st := do(h, "GET", "/api/state", "ms_e2e", "").Body.String()
	for _, want := range []string{"line one", "line two"} {
		if !strings.Contains(st, want) {
			t.Errorf("state should replay %q, got %s", want, st)
		}
	}
}

func TestStoreIsWrittenPromptly(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewStore(dir)
	s.Append("x")
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(filepath.Join(dir, ".setup-state.json")); err == nil && strings.Contains(string(b), "\"x\"") {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Error("the transcript was not persisted")
}

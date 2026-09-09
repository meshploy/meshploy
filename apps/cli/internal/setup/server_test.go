package setup

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeRunner struct {
	lines   []string
	err     error
	started chan struct{} // closed on first Run
	release chan struct{} // Run blocks until closed
}

func (f *fakeRunner) Run(_ context.Context, _ Answers, out func(string)) error {
	if f.started != nil {
		close(f.started)
	}
	for _, l := range f.lines {
		out(l)
	}
	if f.release != nil {
		<-f.release
	}
	return f.err
}

func newTestServer(t *testing.T, runner Runner) (*Server, http.Handler) {
	t.Helper()
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	res := fakeResolver{hosts: map[string][]string{
		"console.example.com": {"203.0.113.10"},
		"api.example.com":     {"203.0.113.10"},
	}}
	s := NewServer(store, "ms_token", res, runner)
	return s, s.Handler()
}

func do(h http.Handler, method, path, token, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		r.Header.Set("X-Setup-Token", token)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// This server writes .env, generates config and runs compose, on a public IP,
// over plain HTTP. The token is the only thing in front of it.
func TestEveryAPIRouteRequiresTheToken(t *testing.T) {
	_, h := newTestServer(t, &fakeRunner{})

	for _, c := range []struct{ method, path string }{
		{"GET", "/api/state"},
		{"POST", "/api/check-domain"},
		{"POST", "/api/answers"},
		{"POST", "/api/install"},
	} {
		if got := do(h, c.method, c.path, "", "{}").Code; got != http.StatusForbidden {
			t.Errorf("%s %s without a token: want 403, got %d", c.method, c.path, got)
		}
		if got := do(h, c.method, c.path, "ms_wrong", "{}").Code; got != http.StatusForbidden {
			t.Errorf("%s %s with a wrong token: want 403, got %d", c.method, c.path, got)
		}
	}
}

// The page itself is not gated — it has to render in order to ask for the
// token. It must carry nothing sensitive, which is why state lives behind /api.
func TestPageIsServedWithoutAToken(t *testing.T) {
	_, h := newTestServer(t, &fakeRunner{})
	w := do(h, "GET", "/", "", "")
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	if strings.Contains(w.Body.String(), "ms_token") {
		t.Error("the page must not embed the setup token")
	}
}

// Checking a domain must not commit it: the operator is expected to try one,
// see what resolves, and change it. That loop is the point of the screen.
func TestCheckDomainDoesNotSaveAnswers(t *testing.T) {
	s, h := newTestServer(t, &fakeRunner{})

	w := do(h, "POST", "/api/check-domain", "ms_token",
		`{"domain":"example.com","dns_mode":"ondemand","public_ip":"203.0.113.10"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body)
	}
	var st DomainStatus
	if err := json.Unmarshal(w.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if !st.Ready {
		t.Errorf("records point at the server; want ready, got %q", st.Reason)
	}
	if saved := s.store.Get().Answers.Domain; saved != "" {
		t.Errorf("checking must not persist the domain, got %q", saved)
	}
}

func TestAnswersAreValidated(t *testing.T) {
	_, h := newTestServer(t, &fakeRunner{})

	for _, body := range []string{
		`{"dns_mode":"ondemand","public_ip":"203.0.113.10"}`,                   // no domain
		`{"domain":"example.com","dns_mode":"nonsense","public_ip":"1.2.3.4"}`, // bad mode
		`{"domain":"example.com","dns_mode":"ondemand"}`,                       // no IP
	} {
		if got := do(h, "POST", "/api/answers", "ms_token", body).Code; got != http.StatusBadRequest {
			t.Errorf("want 400 for %s, got %d", body, got)
		}
	}

	good := `{"domain":"example.com","dns_mode":"ondemand","public_ip":"203.0.113.10"}`
	if got := do(h, "POST", "/api/answers", "ms_token", good).Code; got != http.StatusOK {
		t.Errorf("want 200 for valid answers, got %d", got)
	}
}

// A browser that reconnects mid-install must see the output it missed, not an
// empty pane — otherwise a reload looks like the install stopped.
func TestInstallReplaysTheTranscriptThenStreams(t *testing.T) {
	s, h := newTestServer(t, &fakeRunner{lines: []string{"starting postgres", "done"}})
	_ = s.store.Update(func(st *State) {
		st.Answers = Answers{Domain: "example.com", DNSMode: "ondemand", PublicIP: "203.0.113.10"}
		st.Log = []string{"earlier line from a previous connection"}
	})

	w := do(h, "POST", "/api/install", "ms_token", "")
	body := w.Body.String()

	for _, want := range []string{"earlier line from a previous connection", "starting postgres", "event: done"} {
		if !strings.Contains(body, want) {
			t.Errorf("stream missing %q\n%s", want, body)
		}
	}
	if got := s.store.Get().Phase; got != PhaseVerify {
		t.Errorf("a finished install advances to %q, got %q", PhaseVerify, got)
	}
}

// A failure must be recorded, so resuming shows why rather than silently
// starting over.
func TestFailedInstallIsRecorded(t *testing.T) {
	s, h := newTestServer(t, &fakeRunner{lines: []string{"pulling"}, err: errors.New("compose exited 1")})
	_ = s.store.Update(func(st *State) {
		st.Answers = Answers{Domain: "example.com", DNSMode: "ondemand", PublicIP: "203.0.113.10"}
	})

	w := do(h, "POST", "/api/install", "ms_token", "")
	if !strings.Contains(w.Body.String(), "event: failed") {
		t.Errorf("the browser must be told it failed:\n%s", w.Body.String())
	}
	if got := s.store.Get().Failed; got != "compose exited 1" {
		t.Errorf("the reason should be persisted, got %q", got)
	}
}

// Two tabs, or an impatient double-click, must not start two compose runs
// against the same directory.
func TestConcurrentInstallsAreRefused(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	s, h := newTestServer(t, &fakeRunner{started: started, release: release})
	_ = s.store.Update(func(st *State) {
		st.Answers = Answers{Domain: "example.com", DNSMode: "ondemand", PublicIP: "203.0.113.10"}
	})

	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); do(h, "POST", "/api/install", "ms_token", "") }()
	<-started

	if got := do(h, "POST", "/api/install", "ms_token", "").Code; got != http.StatusConflict {
		t.Errorf("a second install must be refused with 409, got %d", got)
	}
	close(release)
	wg.Wait()
}

// Installing before the answers are valid must be refused, not attempted with
// an empty domain — which would generate a broken Headscale config.
func TestInstallRefusedWithoutAnswers(t *testing.T) {
	_, h := newTestServer(t, &fakeRunner{})
	if got := do(h, "POST", "/api/install", "ms_token", "").Code; got != http.StatusBadRequest {
		t.Errorf("want 400, got %d", got)
	}
}

// Finishing setup must stop the installer. A privileged, token-gated installer
// that keeps listening after it is needed is a permanent liability.
func TestCompleteStopsTheServerAndClearsState(t *testing.T) {
	s, h := newTestServer(t, &fakeRunner{})
	_ = s.store.Update(func(st *State) {
		st.Answers = Answers{Domain: "example.com", DNSMode: "ondemand", PublicIP: "203.0.113.10"}
		st.Phase = PhaseVerify
	})

	select {
	case <-s.Done():
		t.Fatal("the server should still be serving before setup completes")
	default:
	}

	if got := do(h, "POST", "/api/complete", "ms_token", "{}").Code; got != http.StatusOK {
		t.Fatalf("complete: want 200, got %d", got)
	}

	select {
	case <-s.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("finishing setup did not stop the server")
	}

	// A later run must start clean rather than resume a finished install.
	if got := s.store.Get().Phase; got != PhaseAnswers {
		t.Errorf("state should have been cleared, phase is %q", got)
	}
}

// Completing is token-gated like everything else — otherwise a passer-by could
// shut the installer down mid-setup.
func TestCompleteRequiresTheToken(t *testing.T) {
	_, h := newTestServer(t, &fakeRunner{})
	if got := do(h, "POST", "/api/complete", "", "{}").Code; got != http.StatusForbidden {
		t.Errorf("want 403, got %d", got)
	}
}

// Two tabs, or a double-click, must not panic on a second close.
func TestCompleteIsIdempotent(t *testing.T) {
	s, h := newTestServer(t, &fakeRunner{})
	do(h, "POST", "/api/complete", "ms_token", "{}")
	do(h, "POST", "/api/complete", "ms_token", "{}")
	select {
	case <-s.Done():
	default:
		t.Fatal("expected the server to be done")
	}
}

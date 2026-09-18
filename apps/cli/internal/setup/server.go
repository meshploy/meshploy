package setup

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
)

// Runner performs the actual install. Injected so the HTTP surface can be
// tested without provisioning a machine.
type Runner interface {
	// Run streams progress through out and returns when the install finishes.
	Run(ctx context.Context, a Answers, out func(string)) error
}

// Server is the browser-driven installer.
//
// Every /api route is gated on the setup token. The service writes .env,
// generates config and drives compose, and it listens on a public IP over plain
// HTTP because no certificate exists yet — so the token is the only thing
// standing between a stranger and a privileged installer.
type Server struct {
	store    *Store
	token    string
	resolver Resolver
	runner   Runner

	// run is the install in flight, if any. It is owned by the server rather
	// than by the request that started it, so a second browser tab watches the
	// one run instead of starting a second compose run against the same
	// directory - and closing the browser does not kill an install half way.
	mu  sync.Mutex
	run *installRun

	// done is closed when the operator leaves for the console. A privileged
	// installer that keeps listening afterwards is a permanent liability, not a
	// convenience, so finishing setup is what stops it.
	done     chan struct{}
	doneOnce sync.Once
}

func NewServer(store *Store, token string, resolver Resolver, runner Runner) *Server {
	return &Server{
		store: store, token: token, resolver: resolver, runner: runner,
		done: make(chan struct{}),
	}
}

// Done is closed once setup is finished and the server should stop.
func (s *Server) Done() <-chan struct{} { return s.done }

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", s.page)
	mux.Handle("GET /api/state", s.auth(http.HandlerFunc(s.getState)))
	mux.Handle("POST /api/check-domain", s.auth(http.HandlerFunc(s.checkDomain)))
	mux.Handle("POST /api/answers", s.auth(http.HandlerFunc(s.saveAnswers)))
	mux.Handle("POST /api/install", s.auth(http.HandlerFunc(s.install)))
	mux.Handle("POST /api/complete", s.auth(http.HandlerFunc(s.complete)))
	return mux
}

// auth gates a route on the setup token.
//
// Compared in constant time: it is a bearer credential, and an early-returning
// comparison leaks its prefix to anyone who can time the request. An unset
// token means the server was started without one, which is refused at startup
// rather than silently serving an unauthenticated installer.
func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Header only. A query-string fallback existed for EventSource, which
		// cannot set headers -- the page now reads the stream with fetch, so the
		// token never has to travel somewhere access logs record it.
		got := r.Header.Get("X-Setup-Token")
		if s.token == "" ||
			subtle.ConstantTimeCompare([]byte(got), []byte(s.token)) != 1 {
			writeJSON(w, http.StatusForbidden, map[string]string{
				"error": "invalid or missing setup token",
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) page(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// Deliberately no cache: the operator reloads this page to recover from a
	// failed step, and a cached shell showing stale state is the opposite of
	// what this screen is for.
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(wizardHTML)
}

func (s *Server) getState(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.store.Get())
}

func (s *Server) checkDomain(w http.ResponseWriter, r *http.Request) {
	var in Answers
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	// Checking does not commit anything. The operator is expected to try a
	// domain, see what resolves, and change it — that loop is the point.
	writeJSON(w, http.StatusOK, CheckDomain(r.Context(), s.resolver, in.Domain, in.PublicIP, in.DNSMode))
}

func (s *Server) saveAnswers(w http.ResponseWriter, r *http.Request) {
	var in Answers
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if err := validateAnswers(in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := s.store.Update(func(st *State) { st.Answers = in }); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, s.store.Get())
}

// install runs the installer and streams its output as Server-Sent Events.
//
// The persisted transcript is replayed first, so a browser that reconnects
// mid-install sees the output it missed rather than an empty pane.
func (s *Server) install(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "streaming unsupported"})
		return
	}

	st := s.store.Get()
	if err := validateAnswers(st.Answers); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	// One run, however many tabs ask. A second ask watches the first.
	s.mu.Lock()
	run := s.run
	if run == nil || run.finished() {
		run = newInstallRun(st.Log)
		s.run = run
		go s.doInstall(run, st.Answers)
	}
	s.mu.Unlock()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	send := func(event, data string) {
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
		flusher.Flush()
	}

	backlog, lines, unsubscribe := run.watch()
	defer unsubscribe()
	for _, line := range backlog {
		send("log", line)
	}

	drain := func() {
		for {
			select {
			case line := <-lines:
				send("log", line)
			default:
				return
			}
		}
	}
	for {
		select {
		case line := <-lines:
			send("log", line)
		case <-run.done:
			drain()
			if err := run.failure(); err != nil {
				send("failed", err.Error())
			} else {
				send("done", "")
			}
			return
		case <-r.Context().Done():
			// The browser went away: a reload, a closed tab, a dropped
			// connection. The install keeps running - it writes .env and drives
			// compose, and stopping it here would leave the machine half
			// installed. The transcript is on disk, so a reload replays it.
			return
		}
	}
}

// doInstall runs the installer to the end, whatever the browser does.
func (s *Server) doInstall(run *installRun, a Answers) {
	_ = s.store.Update(func(x *State) { x.Phase = PhaseInstall; x.Failed = "" })

	// context.Background(), not the request's: this outlives whoever started
	// it. It ends when this process does, which is also when the installer's
	// own child processes end.
	err := s.runner.Run(context.Background(), a, func(line string) {
		s.store.Append(line)
		run.append(line)
	})
	if err != nil {
		_ = s.store.Update(func(x *State) { x.Failed = err.Error() })
	} else {
		_ = s.store.Update(func(x *State) { x.Phase = PhaseVerify; x.Failed = "" })
	}
	run.finish(err)
}

// complete ends setup: the state file is removed so a later run starts clean
// rather than resuming a finished install, and the server is told to stop.
//
// Deliberately does not check that a certificate exists. In on-demand mode one
// is issued per hostname on first request, so requiring it would strand an
// operator whose install is in fact correct — the failure this whole feature
// exists to remove.
func (s *Server) complete(w http.ResponseWriter, _ *http.Request) {
	if err := s.store.Clear(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "complete"})
	s.doneOnce.Do(func() { close(s.done) })
}

func validateAnswers(a Answers) error {
	if a.Domain == "" {
		return fmt.Errorf("a domain is required")
	}
	if a.DNSMode != "delegation" && a.DNSMode != "ondemand" {
		return fmt.Errorf("dns_mode must be 'delegation' or 'ondemand'")
	}
	if a.PublicIP == "" {
		return fmt.Errorf("a public IP is required")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

package setup

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/meshploy/apps/cli/internal/migrate/dokploy"
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
	// planner is nil when this build cannot plan a migration; the wizard then
	// shows no migration step.
	planner Planner

	// plan is the last plan read from the host. Reading it takes a while on a
	// large server, so the wizard asks for it once and reviews it as long as
	// it likes.
	planMu sync.Mutex
	plan   *dokploy.Plan

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

func NewServer(store *Store, token string, resolver Resolver, runner Runner, planner Planner) *Server {
	return &Server{
		store: store, token: token, resolver: resolver, runner: runner, planner: planner,
		done: make(chan struct{}),
	}
}

// Done is closed once setup is finished and the server should stop.
func (s *Server) Done() <-chan struct{} { return s.done }

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", s.page)
	mux.Handle("GET /assets/fonts/", fonts())
	mux.Handle("GET /api/state", s.auth(http.HandlerFunc(s.getState)))
	mux.Handle("POST /api/check-domain", s.auth(http.HandlerFunc(s.checkDomain)))
	mux.Handle("POST /api/answers", s.auth(http.HandlerFunc(s.saveAnswers)))
	mux.Handle("GET /api/migration", s.auth(http.HandlerFunc(s.getMigration)))
	mux.Handle("POST /api/migration/plan", s.auth(http.HandlerFunc(s.planMigration)))
	mux.Handle("POST /api/migration", s.auth(http.HandlerFunc(s.saveMigration)))
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

// getMigration reports what was found on this server: nothing, or a platform
// with its version and whether it can be planned. The plan itself is read only
// when the operator asks for it.
func (s *Server) getMigration(w http.ResponseWriter, r *http.Request) {
	out := s.migrationState()
	if s.planner == nil {
		writeJSON(w, http.StatusOK, out)
		return
	}
	s.planMu.Lock()
	cached := s.plan
	s.planMu.Unlock()
	if cached != nil {
		out.Plan = cached
		fillDetection(&out, *cached)
		writeJSON(w, http.StatusOK, out)
		return
	}
	found, err := s.planner.Detect(r.Context())
	if err != nil {
		out.Note = err.Error()
		writeJSON(w, http.StatusOK, out)
		return
	}
	fillDetection(&out, found)
	writeJSON(w, http.StatusOK, out)
}

// planMigration reads the whole platform and returns the plan. Separate from
// the detection above because on a server like a busy Dokploy host it takes
// long enough that the wizard has to ask for it deliberately.
func (s *Server) planMigration(w http.ResponseWriter, r *http.Request) {
	if s.planner == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "this build cannot plan a migration"})
		return
	}
	plan, err := s.planner.Plan(r.Context())
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.planMu.Lock()
	s.plan = &plan
	s.planMu.Unlock()

	out := s.migrationState()
	out.Plan = &plan
	fillDetection(&out, plan)
	writeJSON(w, http.StatusOK, out)
}

// saveMigration records the operator's answers, checked against the plan, and
// writes both where the migration stages read them.
func (s *Server) saveMigration(w http.ResponseWriter, r *http.Request) {
	var in MigrationChoices
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	in.SavedAt = time.Now().UTC()

	if !in.Skipped {
		s.planMu.Lock()
		plan := s.plan
		s.planMu.Unlock()
		if plan == nil {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "read the plan before saving choices for it"})
			return
		}
		if err := validateChoices(*plan, in); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		in = withDefaults(*plan, in)
		if err := saveConfirmedPlan(*plan, in); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	}
	if err := s.store.Update(func(st *State) { st.Migration = &in }); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	out := s.migrationState()
	s.planMu.Lock()
	out.Plan = s.plan
	s.planMu.Unlock()
	if out.Plan != nil {
		fillDetection(&out, *out.Plan)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) migrationState() Migration {
	return Migration{Choices: s.store.Get().Migration}
}

// fillDetection copies what detection found into the wizard's view.
func fillDetection(out *Migration, plan dokploy.Plan) {
	if !plan.Detection.Dokploy {
		return
	}
	out.Platform = "Dokploy"
	out.Version = plan.Detection.Version
	out.Supported = plan.Detection.Supported
	if out.Note == "" {
		out.Note = plan.Detection.SupportNote
	}
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

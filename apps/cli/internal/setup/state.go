package setup

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Phase is how far the install has got. Persisted, so closing the browser
// mid-install resumes rather than restarting — a wizard that can only be
// completed in one sitting trades one dead end for another.
type Phase string

const (
	PhaseAnswers  Phase = "answers"  // collecting domain / DNS mode
	PhaseInstall  Phase = "install"  // writing config, running compose
	PhaseVerify   Phase = "verify"   // DNS and certificate checks
	PhaseComplete Phase = "complete" // console is up; this server should stop
)

// Answers is what the wizard collects. Only the first two are real questions;
// the rest are defaults with an override, and none of them are secrets that
// would be unsafe over the plaintext handover connection.
type Answers struct {
	Domain   string `json:"domain"`
	DNSMode  string `json:"dns_mode"` // delegation | ondemand
	PublicIP string `json:"public_ip"`
	MeshIP   string `json:"mesh_ip"`
}

// State is the whole of what survives a browser reload.
type State struct {
	Phase   Phase   `json:"phase"`
	Answers Answers `json:"answers"`

	// Log is the install transcript, replayed to a reconnecting browser so a
	// reload does not lose the output the operator was reading.
	Log []string `json:"log"`

	// Failed records why an install stopped, so resuming can show it rather
	// than starting again silently.
	Failed    string    `json:"failed,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Store persists State next to the rest of the install.
//
// Guarded by a mutex because the HTTP handlers and the install goroutine both
// touch it, and written by rename so a crash mid-write cannot leave a truncated
// file that makes an install unresumable.
type Store struct {
	path string
	mu   sync.Mutex
	st   State
}

func NewStore(dir string) (*Store, error) {
	s := &Store{path: filepath.Join(dir, ".setup-state.json")}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) load() error {
	b, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		s.st = State{Phase: PhaseAnswers}
		return nil
	}
	if err != nil {
		return fmt.Errorf("read setup state: %w", err)
	}
	if err := json.Unmarshal(b, &s.st); err != nil {
		// A corrupt state file must not brick the installer. Starting over is
		// recoverable; refusing to start is not.
		s.st = State{Phase: PhaseAnswers}
		return nil
	}
	if s.st.Phase == "" {
		s.st.Phase = PhaseAnswers
	}
	return nil
}

// Get returns a copy, so callers cannot mutate shared state by accident.
func (s *Store) Get() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.st
	out.Log = append([]string(nil), s.st.Log...)
	return out
}

// Update applies fn to the state and persists the result.
func (s *Store) Update(fn func(*State)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn(&s.st)
	s.st.UpdatedAt = time.Now().UTC()
	return s.persist()
}

// Append adds one transcript line. Separate from Update because the install
// calls it per line and it must stay cheap.
func (s *Store) Append(line string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.st.Log = append(s.st.Log, line)
	s.st.UpdatedAt = time.Now().UTC()
	_ = s.persist() // a lost transcript line must not fail an install
}

// persist writes via a temp file and renames, so an interrupted write leaves
// the previous state intact rather than an unparseable one.
func (s *Store) persist() error {
	b, err := json.MarshalIndent(s.st, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	// 0600: the answers are not secrets, but this sits beside .env and there is
	// no reason for it to be more readable than its neighbours.
	if err := os.WriteFile(tmp, b, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// Clear removes the state file once the console is up, so a later re-run starts
// clean rather than resuming an install that already finished.
func (s *Store) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.st = State{Phase: PhaseAnswers}
	if err := os.Remove(s.path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

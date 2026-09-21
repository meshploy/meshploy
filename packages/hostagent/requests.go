package hostagent

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

// Requests are how the API asks the agent to do something on the host. The
// API writes one file into inbox/; the agent consumes it, runs it, and reports
// in state/. The API runs as root in its container and can write anything
// into inbox/, so the agent trusts nothing about a request but its shape.

// Request types the agent runs.
const (
	RequestMigrateDetect = "migrate.detect"
	RequestMigratePlan   = "migrate.plan"
	// RequestMigrateCredential hands the agent the token it will use against
	// the API for the rest of the migration. Its own step, so the secret
	// crosses this boundary exactly once: every later request carries none.
	RequestMigrateCredential = "migrate.credential"
	// RequestMigratePrepare is stage 1: build the Meshploy side, with nothing
	// serving. No downtime, and Dokploy is not touched.
	RequestMigratePrepare = "migrate.prepare"
	// RequestMigrateMove is stage 2 for one group, named in Args["group"].
	RequestMigrateMove = "migrate.move"
	// RequestMigrateCutover is stage 3: hand over ports 80 and 443.
	RequestMigrateCutover = "migrate.cutover"
	// RequestMigrateRollback undoes one group, or everything when Args["group"]
	// is empty.
	RequestMigrateRollback = "migrate.rollback"
	// RequestMigrateFinish removes what is left of the platform that was
	// migrated. Args["volumes"] = "true" takes its volumes too; Args["plan"] =
	// "true" only reports what would go.
	RequestMigrateFinish = "migrate.finish"
)

// RequestTypes is every type the agent accepts.
var RequestTypes = []string{
	RequestMigrateDetect, RequestMigratePlan, RequestMigrateCredential,
	RequestMigratePrepare, RequestMigrateMove, RequestMigrateCutover, RequestMigrateRollback,
	RequestMigrateFinish,
}

const (
	// MaxRequestBytes bounds a request file.
	MaxRequestBytes = 4 << 10

	requestsDir = "requests"
	migrateDir  = "migrate"
	// CredentialFile is where the API leaves the migration's token, in the
	// inbox. The agent takes it and deletes it; nothing reads it twice.
	//
	// Deliberately unlike RequestMigrateCredential: a file whose name is also a
	// request type invites being read as one, and this one holds a secret.
	CredentialFile = "migration-token.json"
	// DetectFile and PlanFile are the latest results, under state/migrate/.
	DetectFile = "dokploy-detect.json"
	PlanFile   = "dokploy-plan.json"
	// PrepareFile is what stage 1 did, for the console to read; MoveFile the
	// last group moved, CutoverFile the hand-over, RollbackFile the last undo.
	PrepareFile  = "dokploy-prepare.json"
	MoveFile     = "dokploy-move.json"
	CutoverFile  = "dokploy-cutover.json"
	RollbackFile = "dokploy-rollback.json"
	// FinishFile is what stage 4 removed, kept for the operator who wants to
	// know what a server used to run.
	FinishFile = "dokploy-finish.json"
	// StatusFile is where the migration has got to: which stages have run and
	// which groups have moved.
	//
	// The journal on the host is the record of all that, and the API cannot
	// read it - it has no business reading a file the root migrator writes and
	// reads back. So the host writes this summary after every stage, from the
	// journal, and the console reads it like any other result. It is also what
	// makes a migration driven from a terminal show up in the browser.
	StatusFile = "dokploy-status.json"
)

// InboxDir is where the API writes requests.
func InboxDir(dir string) string { return filepath.Join(dir, "inbox") }

// RequestsDir is where the agent reports on each request.
func RequestsDir(dir string) string { return filepath.Join(StateDir(dir), requestsDir) }

// MigrateDir is where migration results are written.
func MigrateDir(dir string) string { return filepath.Join(StateDir(dir), migrateDir) }

// Request is inbox/<type>-<id>.json.
type Request struct {
	ID          string    `json:"id"`
	Type        string    `json:"type"`
	RequestedBy string    `json:"requested_by"`
	RequestedAt time.Time `json:"requested_at"`
	// Args are what the request applies to - a group id, so far. Bounded and
	// validated: the agent trusts nothing about a request but its shape, and
	// this is the only part of one the API chooses the contents of.
	Args map[string]string `json:"args,omitempty"`
}

// Request states.
const (
	RequestRunning   = "running"
	RequestSucceeded = "succeeded"
	RequestFailed    = "failed"
)

// MigrationStatus is where a migration has got to, as the console follows it.
type MigrationStatus struct {
	UpdatedAt time.Time `json:"updated_at"`
	// Prepared, CutOver and Finished are the stages that have happened. After
	// Finished there is nothing left to undo.
	Prepared bool `json:"prepared"`
	CutOver  bool `json:"cut_over"`
	Finished bool `json:"finished"`
	// Groups is every group in the confirmed plan, in the order it would move.
	Groups []GroupProgress `json:"groups"`
}

// GroupProgress is one group of the plan and where it has got to.
type GroupProgress struct {
	ID      string     `json:"id"`
	Name    string     `json:"name"`
	Members []string   `json:"members,omitempty"`
	Moved   bool       `json:"moved"`
	MovedAt *time.Time `json:"moved_at,omitempty"`
	// Error is why the last attempt stopped, when one did. A group that failed
	// put itself back, so this is a reason to read rather than damage to
	// repair.
	Error string `json:"error,omitempty"`
	// CanMove is false while one of its members has a question nobody has
	// answered; Blockers says which.
	CanMove  bool     `json:"can_move"`
	Blockers []string `json:"blockers,omitempty"`
	// Data is what the group carries, and Downtime how long its applications
	// are expected to be unavailable while it moves.
	Data     []string `json:"data,omitempty"`
	Downtime string   `json:"downtime,omitempty"`
}

// RequestStatus is state/requests/<id>.json.
type RequestStatus struct {
	ID          string     `json:"id"`
	Type        string     `json:"type"`
	State       string     `json:"state"`
	RequestedBy string     `json:"requested_by"`
	StartedAt   time.Time  `json:"started_at"`
	FinishedAt  *time.Time `json:"finished_at,omitempty"`
	Error       string     `json:"error,omitempty"`
}

var (
	requestID   = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	requestName = regexp.MustCompile(`^([a-z]+\.[a-z]+)-([0-9a-f-]{36})\.json$`)
	requestArg  = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)
)

// RequestFileName is the inbox file name for a request.
func RequestFileName(req Request) string { return req.Type + "-" + req.ID + ".json" }

// ValidateRequest checks a request's fields; name is its file name.
func ValidateRequest(req Request, name string) error {
	known := false
	for _, t := range RequestTypes {
		known = known || req.Type == t
	}
	switch {
	case !known:
		return fmt.Errorf("unknown request type %q", req.Type)
	case !requestID.MatchString(req.ID):
		return errors.New("request id is not a UUID")
	case name != "" && name != RequestFileName(req):
		return errors.New("request file name does not match its contents")
	case len(req.RequestedBy) > 128:
		return errors.New("requested_by is too long")
	case len(req.Args) > 8:
		return errors.New("too many arguments")
	}
	for k, v := range req.Args {
		if !requestArg.MatchString(k) {
			return fmt.Errorf("argument %q is not a name", k)
		}
		if len(v) > 200 {
			return fmt.Errorf("argument %q is too long", k)
		}
	}
	return nil
}

// ReadRequestFile reads an inbox entry the way the agent must: a regular file,
// not a link, within the size limit, named for its type and id.
func ReadRequestFile(path string) (Request, error) {
	var req Request
	if !requestName.MatchString(filepath.Base(path)) {
		return req, errors.New("not a request file name")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return req, err
	}
	if !info.Mode().IsRegular() {
		return req, errors.New("not a regular file")
	}
	if info.Size() > MaxRequestBytes {
		return req, errors.New("request file is too large")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return req, err
	}
	dec := json.NewDecoder(bytesReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		return req, fmt.Errorf("request is not valid JSON: %w", err)
	}
	return req, ValidateRequest(req, filepath.Base(path))
}

// ReadRequestStatus loads one request's status, or nil if the agent has not
// picked it up.
func ReadRequestStatus(dir, id string) (*RequestStatus, error) {
	if !requestID.MatchString(id) {
		return nil, errors.New("request id is not a UUID")
	}
	return readJSON[RequestStatus](filepath.Join(RequestsDir(dir), id+".json"))
}

// ReadResult loads a migration result as raw JSON, or nil when there is none.
// The API passes it through; its shape belongs to the CLI that wrote it.
func ReadResult(dir, file string) (json.RawMessage, *time.Time, error) {
	path := filepath.Join(MigrateDir(dir), file)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 16<<20 {
		return nil, nil, errors.New(file + " is not a result")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	if !json.Valid(b) {
		return nil, nil, errors.New(file + " is not valid JSON")
	}
	at := info.ModTime().UTC()
	return b, &at, nil
}

// ── The migration's credential ───────────────────────────────────────────────
//
// The migrator runs as root on the host and creates everything in Meshploy
// through the API, which means it needs to authenticate. It is given an agent
// principal's token: a first-class, attributable, revocable identity that does
// not expire, so it survives a migration that runs over days.
//
// The API writes this file into the inbox. The agent reads it once, keeps it
// where only root can read it, and deletes this copy. The inbox is the only
// part of the host directory the API can write, and state/ is mounted into the
// API read-only, so the API can never read the token back.

// Credential is inbox/migrate.credential.
type Credential struct {
	// Token is the agent token, "magt-…".
	Token string `json:"token"`
	// OrgID is the organisation the migration creates into.
	OrgID string `json:"org_id"`
	// AgentID and TokenID are what finish revokes.
	AgentID string `json:"agent_id"`
	TokenID string `json:"token_id"`
	// BaseURL is where the API answers from the host.
	BaseURL   string    `json:"base_url"`
	WrittenAt time.Time `json:"written_at"`
}

// WriteCredential leaves a credential in the inbox for the agent, written under
// a temporary name and renamed so the agent never reads half of it.
func WriteCredential(dir string, c Credential) error {
	inbox := InboxDir(dir)
	if err := os.MkdirAll(inbox, 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(c)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(inbox, ".credential-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(inbox, CredentialFile))
}

// TakeCredential reads the credential and removes it, so it is consumed once.
// A missing file returns nil: the agent was asked for something it was never
// given, which the caller reports rather than crashing on.
func TakeCredential(dir string) (*Credential, error) {
	path := filepath.Join(InboxDir(dir), CredentialFile)
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	// Removed before it is parsed: a malformed credential must not be left
	// lying about for a later run to find.
	if err := os.Remove(path); err != nil {
		return nil, err
	}
	var c Credential
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("read credential: %w", err)
	}
	if c.Token == "" || c.OrgID == "" {
		return nil, errors.New("credential is missing its token or organisation")
	}
	return &c, nil
}

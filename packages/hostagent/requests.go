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
)

// RequestTypes is every type the agent accepts.
var RequestTypes = []string{RequestMigrateDetect, RequestMigratePlan, RequestMigrateCredential}

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
}

// Request states.
const (
	RequestRunning   = "running"
	RequestSucceeded = "succeeded"
	RequestFailed    = "failed"
)

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

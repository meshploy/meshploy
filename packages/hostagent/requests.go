package hostagent

import (
	"encoding/json"
	"errors"
	"fmt"
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
)

// RequestTypes is every type the agent accepts.
var RequestTypes = []string{RequestMigrateDetect, RequestMigratePlan}

const (
	// MaxRequestBytes bounds a request file.
	MaxRequestBytes = 4 << 10

	requestsDir = "requests"
	migrateDir  = "migrate"
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

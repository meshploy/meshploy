package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/meshploy/packages/hostagent"
)

// Migration planning runs on the host, through the host agent: the API writes a
// request into the agent's inbox and reads the result the agent leaves in its
// state directory. The API never reads Docker or another platform's database
// itself.

var (
	ErrMigrationNotOwner     = errors.New("only the owner of this server can plan migrating it")
	ErrHostAgentNotReporting = errors.New("the host agent is not reporting, so nothing would run the request; on the gateway, run: sudo meshploy host start")
	ErrUnknownHostRequest    = errors.New("unknown request")
)

// MigrationState is what the console shows about migrating this server.
type MigrationState struct {
	AgentReporting bool `json:"agent_reporting"`
	// Detect and Plan are the latest results as the agent wrote them, or null.
	Detect   json.RawMessage `json:"detect"`
	DetectAt *time.Time      `json:"detect_at,omitempty"`
	Plan     json.RawMessage `json:"plan"`
	PlanAt   *time.Time      `json:"plan_at,omitempty"`
	// Requests is the latest request of each type, by type.
	Requests map[string]HostRequestState `json:"requests"`
}

// HostRequestState is a request as the console follows it: queued until the
// agent picks it up, then running, succeeded or failed.
type HostRequestState struct {
	ID          string     `json:"id"`
	State       string     `json:"state"`
	RequestedAt time.Time  `json:"requested_at"`
	FinishedAt  *time.Time `json:"finished_at,omitempty"`
	Error       string     `json:"error,omitempty"`
}

func (s *SystemService) hostDir() string {
	if s.cfg == nil {
		return ""
	}
	return s.cfg.HostDir
}

// RequestMigration asks the host agent to detect or plan. Only the instance
// owner may: the plan names every app, domain and path on the server.
func (s *SystemService) RequestMigration(ctx context.Context, userID uuid.UUID, kind string) (HostRequestState, error) {
	owner, err := s.IsInstanceOwner(ctx, userID)
	if err != nil {
		return HostRequestState{}, err
	}
	if !owner {
		return HostRequestState{}, ErrMigrationNotOwner
	}
	var reqType string
	switch kind {
	case "detect":
		reqType = hostagent.RequestMigrateDetect
	case "plan":
		reqType = hostagent.RequestMigratePlan
	default:
		return HostRequestState{}, ErrUnknownHostRequest
	}
	if s.hostDir() == "" || !s.HostAgentStatus().Reporting {
		return HostRequestState{}, ErrHostAgentNotReporting
	}

	req := hostagent.Request{ID: uuid.NewString(), Type: reqType, RequestedBy: userID.String(), RequestedAt: time.Now().UTC()}
	if err := hostagent.ValidateRequest(req, ""); err != nil {
		return HostRequestState{}, err
	}
	data, err := json.Marshal(req)
	if err != nil {
		return HostRequestState{}, err
	}
	inbox := hostagent.InboxDir(s.hostDir())
	// Written under a temporary name and renamed, so the agent never reads
	// half a request.
	tmp, err := os.CreateTemp(inbox, ".request-*")
	if err != nil {
		return HostRequestState{}, err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return HostRequestState{}, err
	}
	if err := tmp.Close(); err != nil {
		return HostRequestState{}, err
	}
	if err := os.Rename(tmp.Name(), filepath.Join(inbox, hostagent.RequestFileName(req))); err != nil {
		return HostRequestState{}, err
	}
	return HostRequestState{ID: req.ID, State: "queued", RequestedAt: req.RequestedAt}, nil
}

// GetMigrationState returns the latest results and requests. Owner only.
func (s *SystemService) GetMigrationState(ctx context.Context, userID uuid.UUID) (MigrationState, error) {
	out := MigrationState{Requests: map[string]HostRequestState{}}
	owner, err := s.IsInstanceOwner(ctx, userID)
	if err != nil {
		return out, err
	}
	if !owner {
		return out, ErrMigrationNotOwner
	}
	dir := s.hostDir()
	if dir == "" {
		return out, nil
	}
	out.AgentReporting = s.HostAgentStatus().Reporting
	if out.Detect, out.DetectAt, err = hostagent.ReadResult(dir, hostagent.DetectFile); err != nil {
		return out, err
	}
	if out.Plan, out.PlanAt, err = hostagent.ReadResult(dir, hostagent.PlanFile); err != nil {
		return out, err
	}

	latest := func(kind string, st HostRequestState) {
		if cur, ok := out.Requests[kind]; !ok || st.RequestedAt.After(cur.RequestedAt) {
			out.Requests[kind] = st
		}
	}
	// Picked up and run, or running.
	if entries, err := os.ReadDir(hostagent.RequestsDir(dir)); err == nil {
		for _, e := range entries {
			id := trimJSON(e.Name())
			st, err := hostagent.ReadRequestStatus(dir, id)
			if err != nil || st == nil {
				continue
			}
			latest(kindOf(st.Type), HostRequestState{ID: st.ID, State: st.State, RequestedAt: st.StartedAt, FinishedAt: st.FinishedAt, Error: st.Error})
		}
	}
	// Still waiting for the agent.
	if entries, err := os.ReadDir(hostagent.InboxDir(dir)); err == nil {
		for _, e := range entries {
			req, err := hostagent.ReadRequestFile(filepath.Join(hostagent.InboxDir(dir), e.Name()))
			if err != nil {
				continue
			}
			latest(kindOf(req.Type), HostRequestState{ID: req.ID, State: "queued", RequestedAt: req.RequestedAt})
		}
	}
	return out, nil
}

func kindOf(reqType string) string {
	switch reqType {
	case hostagent.RequestMigrateDetect:
		return "detect"
	case hostagent.RequestMigratePlan:
		return "plan"
	}
	return reqType
}

func trimJSON(name string) string {
	if len(name) > 5 && name[len(name)-5:] == ".json" {
		return name[:len(name)-5]
	}
	return name
}

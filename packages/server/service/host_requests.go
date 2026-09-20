package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	meshdb "github.com/meshploy/packages/db"
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
	// Prepare is what stage 1 created, Move the last group moved, Cutover the
	// hand-over of ports 80 and 443 - each once it has run.
	Prepare   json.RawMessage `json:"prepare"`
	PrepareAt *time.Time      `json:"prepare_at,omitempty"`
	Move      json.RawMessage `json:"move"`
	MoveAt    *time.Time      `json:"move_at,omitempty"`
	Cutover   json.RawMessage `json:"cutover"`
	CutoverAt *time.Time      `json:"cutover_at,omitempty"`
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
func (s *SystemService) RequestMigration(ctx context.Context, userID uuid.UUID, kind string, args map[string]string) (HostRequestState, error) {
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
	case "credential":
		reqType = hostagent.RequestMigrateCredential
	case "prepare":
		reqType = hostagent.RequestMigratePrepare
	case "move":
		reqType = hostagent.RequestMigrateMove
	case "cutover":
		reqType = hostagent.RequestMigrateCutover
	case "rollback":
		reqType = hostagent.RequestMigrateRollback
	default:
		return HostRequestState{}, ErrUnknownHostRequest
	}
	if s.hostDir() == "" || !s.HostAgentStatus().Reporting {
		return HostRequestState{}, ErrHostAgentNotReporting
	}

	// The migrator creates everything through the API, so it needs an identity.
	// It is minted here and left in the inbox, which is the only part of the
	// host directory this container can write; state/ is mounted read-only, so
	// the token cannot be read back once the agent has taken it.
	if reqType == hostagent.RequestMigrateCredential {
		if err := s.issueMigrationCredential(ctx, userID); err != nil {
			return HostRequestState{}, err
		}
	}

	req := hostagent.Request{ID: uuid.NewString(), Type: reqType, RequestedBy: userID.String(),
		RequestedAt: time.Now().UTC(), Args: args}
	// Stage 2 acts on one group, and the console has to say which.
	if reqType == hostagent.RequestMigrateMove && args["group"] == "" {
		return HostRequestState{}, errors.New("which group should move?")
	}
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
	if out.Prepare, out.PrepareAt, err = hostagent.ReadResult(dir, hostagent.PrepareFile); err != nil {
		return out, err
	}
	if out.Move, out.MoveAt, err = hostagent.ReadResult(dir, hostagent.MoveFile); err != nil {
		return out, err
	}
	if out.Cutover, out.CutoverAt, err = hostagent.ReadResult(dir, hostagent.CutoverFile); err != nil {
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

// issueMigrationCredential mints the migration agent's token and leaves it for
// the host agent to take.
func (s *SystemService) issueMigrationCredential(ctx context.Context, userID uuid.UUID) error {
	if s.agents == nil {
		return errors.New("this server cannot mint a migration credential")
	}
	orgID, err := s.instanceOrg(ctx)
	if err != nil {
		return err
	}
	agentID, tokenID, token, err := s.agents.EnsureMigrationAgent(ctx, orgID, userID)
	if err != nil {
		return err
	}
	return hostagent.WriteCredential(s.hostDir(), hostagent.Credential{
		Token:     token,
		OrgID:     orgID.String(),
		AgentID:   agentID.String(),
		TokenID:   tokenID.String(),
		BaseURL:   s.apiBaseURL(),
		WrittenAt: time.Now().UTC(),
	})
}

// instanceOrg is the organisation a migration creates into. CE is single-org by
// design, so it is the one there is; a server with none has nothing to migrate
// into yet, which is a clearer error than a nil id.
func (s *SystemService) instanceOrg(ctx context.Context) (uuid.UUID, error) {
	var org meshdb.Organization
	if err := s.db.WithContext(ctx).Order("created_at ASC").First(&org).Error; err != nil {
		return uuid.Nil, errors.New("no organisation exists yet: register first, then start the migration")
	}
	return org.ID, nil
}

// apiBaseURL is where the agent reaches the API from the host. Not the public
// URL: the agent is on the same machine, and the loopback address needs no DNS,
// no certificate and no edge to be working.
func (s *SystemService) apiBaseURL() string {
	port := 4000
	if s.cfg != nil && s.cfg.APIPort != 0 {
		port = s.cfg.APIPort
	}
	return fmt.Sprintf("http://127.0.0.1:%d", port)
}

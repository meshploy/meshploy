package agent

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/meshploy/packages/hostagent"
	appk8s "github.com/meshploy/packages/server/k8s"
)

// hostFolder is the host agent's folder as the API reaches it, through a
// served agent carrying requests to dir.
func hostFolder(t *testing.T, carry bool) (hostagent.Folder, string) {
	t.Helper()
	api := newAPIServer(t)
	a, _, _ := testAgent(t, api)
	dir := t.TempDir()
	if carry {
		a.CarryHostRequests(dir)
	}
	addr, id := servedAgent(t, a)
	certPEM, _ := id.CertPEM()
	token, _ := id.Token()
	c, err := appk8s.NewAgentCluster()
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Set(appk8s.AgentIdentity{Addr: addr, CertPEM: certPEM, Token: token}); err != nil {
		t.Fatal(err)
	}
	return c.HostFolder(), dir
}

func refused(t *testing.T, what string, err error) {
	t.Helper()
	var r *appk8s.AgentRefusal
	if !errors.As(err, &r) || r.Status != http.StatusForbidden && r.Status != http.StatusBadRequest {
		t.Errorf("%s: %v, want the agent's refusal", what, err)
	}
}

func edgeToServe() hostagent.EdgeSnapshot {
	return hostagent.EdgeSnapshot{
		PublicIP: "203.0.113.10", MeshIP: "100.64.0.2", PlatformElsewhere: true, PlatformAPI: "http://100.64.0.1:4000",
		Domains: []hostagent.EdgeDomain{
			{BaseDomain: "example.com", InternalSubdomain: "internal", DNSMode: hostagent.EdgeDNSDelegation, Primary: true},
		},
	}
}

// What the API leaves for the host agent reaches its inbox, as the host
// agent reads it: a domain.apply, a registry.gc, the edge to serve and the
// DNS providers' tokens, the tokens readable by root only.
func TestTheAgentCarriesTheAPIsRequestsToTheHostAgent(t *testing.T) {
	f, dir := hostFolder(t, true)

	apply := hostagent.Request{ID: uuid.NewString(), Type: hostagent.RequestDomainApply, RequestedBy: "meshploy", RequestedAt: time.Now().UTC()}
	if err := hostagent.QueueRequest(f, apply); err != nil {
		t.Fatalf("domain.apply: %v", err)
	}
	got, err := hostagent.ReadRequestFile(filepath.Join(dir, "inbox", hostagent.RequestFileName(apply)))
	if err != nil || got.ID != apply.ID {
		t.Fatalf("the host agent reads %+v, %v", got, err)
	}
	gc := hostagent.Request{ID: uuid.NewString(), Type: hostagent.RequestRegistryGC, RequestedBy: "meshploy", RequestedAt: time.Now().UTC()}
	if err := hostagent.QueueRequest(f, gc); err != nil {
		t.Fatalf("registry.gc: %v", err)
	}
	pending, err := hostagent.PendingRequests(f)
	if err != nil || len(pending) != 2 {
		t.Fatalf("pending through the agent: %+v, %v", pending, err)
	}

	if err := hostagent.WriteDesiredEdgeSnapshot(f, edgeToServe()); err != nil {
		t.Fatalf("the edge to serve: %v", err)
	}
	snap, err := hostagent.ReadDesiredEdgeSnapshot(hostagent.Local(dir))
	if err != nil || !snap.PlatformElsewhere || snap.Domains[0].BaseDomain != "example.com" {
		t.Fatalf("the host agent reads %+v, %v", snap, err)
	}

	providerID := uuid.NewString()
	if err := f.Put(hostagent.DNSCredentialsFile, []byte(`{"`+providerID+`":"tok"}`), false); err != nil {
		t.Fatalf("the providers' tokens: %v", err)
	}
	info, err := os.Stat(filepath.Join(dir, hostagent.DNSCredentialsFile))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("the tokens are root's whatever the API asked: %v %v", info.Mode(), err)
	}
	if creds, err := hostagent.TakeDNSCredentials(dir); err != nil || creds[providerID] != "tok" {
		t.Fatalf("the host agent takes %v, %v", creds, err)
	}
}

// Only what the API leaves for the host agent is written, and only in the
// shape the host agent takes; a migration is not carried.
func TestTheAgentRefusesWhatTheHostAgentShouldNotGet(t *testing.T) {
	f, dir := hostFolder(t, true)
	id := uuid.NewString()
	plan := `{"id":"` + id + `","type":"migrate.plan","requested_by":"x","requested_at":"2026-11-08T00:00:00Z"}`
	refused(t, "a migration", f.Put("inbox/migrate.plan-"+id+".json", []byte(plan), false))
	refused(t, "the migration's token", f.Put("inbox/"+hostagent.CredentialFile, []byte(`{"token":"magt-x","org_id":"o"}`), true))
	apply := `{"id":"` + id + `","type":"domain.apply","requested_by":"x","requested_at":"2026-11-08T00:00:00Z"}`
	refused(t, "a request under another request's name", f.Put("inbox/registry.gc-"+id+".json", []byte(apply), false))
	refused(t, "a request with fields the host agent does not know", f.Put("inbox/domain.apply-"+id+".json", []byte(strings.Replace(apply, "}", `,"run":"sh"}`, 1)), false))
	refused(t, "a report", f.Put("state/agent.json", []byte(`{}`), false))
	refused(t, "an edge that cannot be served", f.Put(hostagent.EdgeDesiredFile, []byte(`{"domains":[]}`), false))
	refused(t, "a provider that is not one", f.Put(hostagent.DNSCredentialsFile, []byte(`{"../x":"tok"}`), true))
	refused(t, "anything else", f.Put("inbox/notes.txt", []byte("hi"), false))
	if _, err := hostPathOK(f, "../etc/passwd"); err == nil {
		t.Error("a name outside the folder was sent")
	}
	if names, _ := os.ReadDir(filepath.Join(dir, "inbox")); len(names) != 0 {
		t.Fatalf("refused files reached the inbox: %v", names)
	}
}

func hostPathOK(f hostagent.Folder, name string) ([]byte, error) {
	b, _, err := f.Read(name, 1<<20)
	return b, err
}

// The API reads the host agent's reports and the requests' progress, and
// nothing else in the folder: not the migration's credential, not the tokens
// it left, not a link out of the folder.
func TestTheAPIReadsOnlyTheHostAgentsReports(t *testing.T) {
	f, dir := hostFolder(t, true)
	state := filepath.Join(dir, "state")
	os.MkdirAll(filepath.Join(state, "requests"), 0o755)
	os.MkdirAll(filepath.Join(state, "migrate"), 0o755)
	beat := time.Now().UTC().Truncate(time.Second)
	os.WriteFile(filepath.Join(state, hostagent.AgentFile), []byte(`{"version":"1.2.3","heartbeat_at":"`+beat.Format(time.RFC3339)+`"}`), 0o644)
	os.WriteFile(filepath.Join(state, hostagent.FirewallFile), []byte(`{"tool":"ufw","active":true}`), 0o644)
	id := uuid.NewString()
	os.WriteFile(filepath.Join(state, "requests", id+".json"), []byte(`{"id":"`+id+`","type":"domain.apply","state":"succeeded"}`), 0o644)
	os.WriteFile(filepath.Join(state, "migrate", hostagent.RegistryGCFile), []byte(`{"before_bytes":10,"after_bytes":4}`), 0o644)
	os.WriteFile(filepath.Join(state, "migrate", "dokploy-credential.json"), []byte(`{"token":"magt-secret"}`), 0o600)
	os.WriteFile(filepath.Join(state, "migrate", hostagent.PlanFile), []byte(`{}`), 0o600)
	os.MkdirAll(filepath.Join(dir, "inbox"), 0o755)
	os.WriteFile(filepath.Join(dir, hostagent.DNSCredentialsFile), []byte(`{}`), 0o600)
	outside := filepath.Join(t.TempDir(), "secret")
	os.WriteFile(outside, []byte(`{"heartbeat_at":"2030-01-01T00:00:00Z"}`), 0o644)
	os.Symlink(outside, filepath.Join(state, hostagent.ListenersFile))

	agent, fw, err := hostagent.ReadState(f)
	if err != nil || agent == nil || agent.Version != "1.2.3" || !agent.HeartbeatAt.Equal(beat) || fw == nil || fw.Tool != "ufw" {
		t.Fatalf("state through the agent: %+v %+v %v", agent, fw, err)
	}
	if d, _, err := hostagent.ReadDocker(f, time.Now()); d != nil || err != nil {
		t.Fatalf("a report never written: %+v %v", d, err)
	}
	statuses, err := hostagent.RequestStatuses(f)
	if err != nil || len(statuses) != 1 || statuses[0].State != "succeeded" {
		t.Fatalf("request statuses: %+v %v", statuses, err)
	}
	raw, at, err := hostagent.ReadResult(f, hostagent.RegistryGCFile)
	if err != nil || raw == nil || at == nil {
		t.Fatalf("registry.gc's result: %s %v", raw, err)
	}

	for _, name := range []string{
		"state/migrate/dokploy-credential.json",
		"state/migrate/" + hostagent.PlanFile,
		hostagent.DNSCredentialsFile,
		"state/requests",
	} {
		refused(t, "reading "+name, func() error { _, err := hostPathOK(f, name); return err }())
	}
	if _, _, err := hostagent.ReadListeners(f, time.Now()); err == nil {
		t.Error("a link out of the folder was followed")
	}
	if _, err := f.List("state/migrate"); err == nil {
		t.Error("listed a folder the API does not follow")
	}
}

// Without a host folder the agent carries nothing, and without an agent the
// API reaches no host.
func TestNoHostFolderNoAgentNothingCarried(t *testing.T) {
	f, _ := hostFolder(t, false)
	refused(t, "an agent not set up to carry", hostagent.QueueRequest(f, hostagent.Request{
		ID: uuid.NewString(), Type: hostagent.RequestDomainApply, RequestedBy: "x", RequestedAt: time.Now(),
	}))

	c, _ := appk8s.NewAgentCluster()
	if _, _, err := c.HostFolder().Read("state/agent.json", 1<<10); !errors.Is(err, appk8s.ErrNoClusterAgent) {
		t.Fatalf("no agent: %v", err)
	}
}

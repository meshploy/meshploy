package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/meshploy/packages/hostagent"
	appk8s "github.com/meshploy/packages/server/k8s"
)

// The host agent's folder, carried for the API. The API writes into inbox/
// the requests the host agent runs, the edge it wants served and the DNS
// providers' tokens that edge names, and reads back what the host agent
// reports in state/. Each name is one this agent knows, and each body is
// checked for its shape before it is left for the host agent, which checks
// it again.

// carriedRequests are the host agent's requests the API may ask for through
// this agent. A migration is not among them: it acts on the machine through
// an API on the same machine.
var carriedRequests = []string{hostagent.RequestDomainApply, hostagent.RequestRegistryGC, hostagent.RequestServerUpgrade}

// readableState are the reports the API reads, under state/.
var readableState = []string{
	hostagent.AgentFile, hostagent.FirewallFile, hostagent.DockerFile, hostagent.ListenersFile, hostagent.FactsFile,
	path.Base(hostagent.EdgeSnapshotFile), hostagent.UpgradeFile,
}

// readableResults are the results the API reads, under state/migrate/.
var readableResults = []string{hostagent.RegistryGCFile, hostagent.DomainApplyFile, hostagent.ServerUpgradeFile}

var (
	requestFile = regexp.MustCompile(`^inbox/[a-z]+\.[a-z]+-[0-9a-f-]{36}\.json$`)
	statusFile  = regexp.MustCompile(`^state/requests/[0-9a-f-]{36}\.json$`)
)

// CarryHostRequests has the agent carry requests to the host agent whose
// folder is dir. Unset, it refuses them.
func (a *Agent) CarryHostRequests(dir string) {
	if dir != "" {
		a.hostDir = hostagent.Local(dir)
	}
}

func (a *Agent) serveHost(w http.ResponseWriter, r *http.Request, rest string) {
	if a.hostDir == nil {
		a.refuseIntent(w, r, http.StatusForbidden, "requests for the host agent, which this cluster agent is not set up to carry")
		return
	}
	kind, escaped, _ := strings.Cut(rest, "/")
	name, err := url.PathUnescape(escaped)
	if err == nil {
		name, err = hostagent.CleanName(name)
	}
	if err != nil {
		a.refuseIntent(w, r, http.StatusBadRequest, "a name outside the host agent's folder")
		return
	}
	switch {
	case kind == "files" && r.Method == http.MethodGet:
		a.readHostFile(w, r, name)
	case kind == "files" && r.Method == http.MethodPut:
		a.putHostFile(w, r, name)
	case kind == "list" && r.Method == http.MethodGet:
		a.listHostFolder(w, r, name)
	default:
		a.refuseIntent(w, r, http.StatusNotFound, "no such request")
	}
}

// readable is whether the API may read name: a report, a request's status,
// a result it follows, or a request still waiting. Nothing else in the folder
// is the API's to read; the migration keeps a credential there.
func readable(name string) bool {
	dir, file := path.Split(name)
	switch dir {
	case "state/":
		return slices.Contains(readableState, file)
	case "state/migrate/":
		return slices.Contains(readableResults, file)
	}
	return statusFile.MatchString(name) || requestFile.MatchString(name)
}

func (a *Agent) readHostFile(w http.ResponseWriter, r *http.Request, name string) {
	if !readable(name) {
		a.refuseIntent(w, r, http.StatusForbidden, "reading "+name+", which is not a report the API reads")
		return
	}
	data, at, err := a.hostDir.Read(name, hostagent.MaxResultBytes)
	if errors.Is(err, fs.ErrNotExist) {
		http.Error(w, name+" is not there", http.StatusNotFound)
		return
	}
	if err != nil {
		a.logf("FAIL  %s %s: %v", r.Method, r.URL.Path, err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set(appk8s.HostModifiedHeader, at.Format(time.RFC3339Nano))
	w.Write(data)
}

func (a *Agent) listHostFolder(w http.ResponseWriter, r *http.Request, name string) {
	if name != "inbox" && name != "state/requests" {
		a.refuseIntent(w, r, http.StatusForbidden, "listing "+name+", which the API does not follow")
		return
	}
	names, err := a.hostDir.List(name)
	if err != nil {
		a.logf("FAIL  %s %s: %v", r.Method, r.URL.Path, err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if names == nil {
		names = []string{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(names)
}

func (a *Agent) putHostFile(w http.ResponseWriter, r *http.Request, name string) {
	data, err := io.ReadAll(io.LimitReader(r.Body, hostagent.MaxEdgeSnapshotBytes+1))
	if err != nil {
		http.Error(w, "read: "+err.Error(), http.StatusBadRequest)
		return
	}
	private, what, err := checkHostFile(name, data)
	if err != nil {
		a.refuseIntent(w, r, http.StatusForbidden, err.Error())
		return
	}
	if err := a.hostDir.Put(name, data, private); err != nil {
		a.logf("FAIL  %s %s: %v", r.Method, r.URL.Path, err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	a.logf("DO    %s %s: %s", r.Method, r.URL.Path, what)
	w.WriteHeader(http.StatusNoContent)
}

// checkHostFile is whether the API may leave data at name, whether it is
// readable by root only, and what it is, for the log. The tokens are always
// root's, whatever the API asked.
func checkHostFile(name string, data []byte) (private bool, what string, err error) {
	if len(data) > hostagent.MaxEdgeSnapshotBytes {
		return false, "", errors.New("a file larger than the host agent takes")
	}
	switch {
	case name == hostagent.EdgeDesiredFile:
		snap, err := hostagent.DecodeEdgeSnapshot(data)
		if err == nil {
			err = snap.Validate()
		}
		if err != nil {
			return false, "", fmt.Errorf("an edge that cannot be served: %v", err)
		}
		return false, fmt.Sprintf("the edge to serve, %d domains", len(snap.Domains)), nil
	case name == hostagent.DNSCredentialsFile:
		var creds hostagent.DNSCredentials
		if err := json.Unmarshal(data, &creds); err != nil {
			return false, "", errors.New("DNS providers' tokens that cannot be read")
		}
		if err := creds.Validate(); err != nil {
			return false, "", err
		}
		return true, fmt.Sprintf("the tokens of %d DNS providers", len(creds)), nil
	case requestFile.MatchString(name):
		req, err := hostagent.ParseRequest(data, path.Base(name))
		if err != nil {
			return false, "", fmt.Errorf("a request the host agent would not take: %v", err)
		}
		if !slices.Contains(carriedRequests, req.Type) {
			return false, "", fmt.Errorf("a %s request, which the API asks for only on its own machine", req.Type)
		}
		return false, "a " + req.Type + " request", nil
	}
	return false, "", errors.New("writing " + name + ", which is not a file the API leaves for the host agent")
}

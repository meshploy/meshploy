package dokploy

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/meshploy/packages/client"
)

// The real Meshploy behind the API the migration engine writes to.
//
// Thin on purpose: the engine decides what to create and in what order, and
// this only translates. Anything that needs a decision belongs in the engine,
// where it can be tested without a server.

// ClientAPI implements API against a running Meshploy.
type ClientAPI struct {
	C     *client.Client
	OrgID string
}

// CreateProject returns the id of a project with this name, making one if there
// is none.
//
// Reused rather than duplicated, because stage 1 may be run again after a
// failure and a second "Acme" project would split the server in half. The
// existing one is matched on name, which is what an operator sees.
func (a ClientAPI) CreateProject(name string) (string, error) {
	existing, err := a.C.ListProjects(a.OrgID)
	if err != nil {
		return "", err
	}
	for _, p := range existing {
		if strings.EqualFold(p.Name, name) {
			return p.ID, nil
		}
	}
	p, err := a.C.CreateProject(a.OrgID, name)
	if err != nil {
		return "", err
	}
	return p.ID, nil
}

// CreateService creates a workload, stopped.
//
// Creating does not deploy: a service is born stopped and only a deploy starts
// it, which is exactly what stage 1 wants. Stage 2 starts the ones whose group
// is moving.
func (a ClientAPI) CreateService(projectID string, spec ServiceSpec) (string, error) {
	body := client.CreateServiceBody{
		Name:    spec.Name,
		Image:   spec.Image,
		Type:    spec.Type,
		GitRepo: spec.GitRepo,
		Branch:  spec.Branch,
		EnvVars: spec.EnvVars,
		Ports:   servicePorts(spec.Ports),
	}
	if spec.Type == "database" {
		// The credentials the data already uses: a dump restored into a
		// database with a different user or name would not be the same
		// database, and the app's connection string names both.
		body.Engine, body.Version = spec.Engine, spec.Version
		body.DBName, body.DBUser, body.DBPassword = spec.DBName, spec.DBUser, spec.Password
	}
	svc, err := a.C.CreateService(a.OrgID, projectID, body)
	if err != nil {
		return "", err
	}
	return svc.ID, nil
}

// servicePorts names the ports a migrated workload listens on. The first is
// primary - it is the one the app's own hostname routes to - and every one is
// public, because each exists precisely because a domain points at it.
func servicePorts(ports []int) []client.ServicePortBody {
	out := make([]client.ServicePortBody, 0, len(ports))
	for i, p := range ports {
		name := "http"
		if i > 0 {
			name = fmt.Sprintf("http-%d", p)
		}
		out = append(out, client.ServicePortBody{
			Name: name, Port: p, IsHTTP: true, IsPrimary: i == 0, IsPublic: true,
		})
	}
	return out
}

func (a ClientAPI) SetEnvVars(projectID, serviceID, env string) error {
	return a.C.SetEnvVars(a.OrgID, projectID, serviceID, env)
}

// CreateRoute creates a route paused: it exists, serves nothing, and gets no
// certificate until its group moves and the console publishes it.
func (a ClientAPI) CreateRoute(projectID string, spec RouteSpec) (string, error) {
	if spec.Hostname == "" {
		return "", fmt.Errorf("a route needs a hostname")
	}
	paused := false
	body := client.CreateRouteBody{
		Hostname:  &spec.Hostname,
		ServiceID: &spec.ServiceID,
		Published: &paused,
		Path:      spec.Path,
		StripPath: spec.StripPath,
	}
	if spec.Port != 0 {
		port := spec.Port
		body.Port = &port
	}
	r, err := a.C.CreateRoute(a.OrgID, projectID, body)
	if err != nil {
		return "", err
	}
	return r.ID, nil
}

// CreateVolume makes a volume, reusing one of the same name in the project: a
// re-run of stage 1 must not leave two volumes where the data can only go in
// one.
func (a ClientAPI) CreateVolume(projectID, name string, storageGB int) (string, error) {
	existing, err := a.C.ListVolumes(a.OrgID, projectID)
	if err != nil {
		return "", err
	}
	for _, v := range existing {
		if strings.EqualFold(v.Name, name) {
			return v.ID, nil
		}
	}
	v, err := a.C.CreateVolume(a.OrgID, projectID, client.CreateVolumeBody{Name: name, StorageGB: storageGB})
	if err != nil {
		return "", err
	}
	return v.ID, nil
}

func (a ClientAPI) AttachVolume(projectID, volumeID, serviceID, mountPath string) error {
	_, err := a.C.AttachVolume(a.OrgID, projectID, volumeID, client.AttachVolumeBody{
		ServiceID: serviceID, MountPath: mountPath,
	})
	return err
}

func (a ClientAPI) CreateConfigFile(projectID, serviceID, name, path, content string) error {
	f, err := a.C.CreateConfigFile(a.OrgID, projectID, name, path, content)
	if err != nil {
		return err
	}
	return a.C.AttachConfigFile(a.OrgID, projectID, f.ID, serviceID)
}

// BuiltinRegistry is where locally built images are carried to: the registry
// Meshploy runs on the gateway, seeded per organisation at install.
//
// Read from the API rather than from the host's .env, so the migrator uses the
// endpoint Meshploy itself believes in. An install without one returns an empty
// registry, and images are then left as they are.
func (a ClientAPI) BuiltinRegistry() (Registry, error) {
	list, err := a.C.ListRegistryIntegrations(a.OrgID)
	if err != nil {
		return Registry{}, err
	}
	for _, r := range list {
		if r.Provider == "builtin" && r.Endpoint != "" {
			return Registry{Endpoint: r.Endpoint}, nil
		}
	}
	return Registry{}, nil
}

// ── Stage 2 ──────────────────────────────────────────────────────────────────

func (a ClientAPI) StartService(projectID, serviceID string) error {
	return a.C.StartService(a.OrgID, projectID, serviceID)
}

func (a ClientAPI) StopService(projectID, serviceID string) error {
	return a.C.StopService(a.OrgID, projectID, serviceID)
}

// ServiceStatus is what the move waits on, and it means a workload that is
// actually up.
//
// The stored status alone does not: Meshploy records "running" the moment the
// start is accepted, which is before the cluster has scheduled anything. A
// move that trusted it switched the domain, and restored a database, into a
// pod that did not exist yet - so a service is only running here once one of
// its pods is ready.
func (a ClientAPI) ServiceStatus(projectID, serviceID string) (string, error) {
	svc, err := a.C.GetService(a.OrgID, projectID, serviceID)
	if err != nil {
		return "", err
	}
	if svc.Status != "running" {
		return svc.Status, nil
	}
	if _, err := a.RunningPod(projectID, serviceID); err != nil {
		return "starting", nil
	}
	return svc.Status, nil
}

func (a ClientAPI) PublishRoute(projectID, routeID string) error {
	_, err := a.C.PublishRoute(a.OrgID, projectID, routeID)
	return err
}

func (a ClientAPI) PauseRoute(projectID, routeID string) error {
	_, err := a.C.PauseRoute(a.OrgID, projectID, routeID)
	return err
}

// ── Stage 2: data ────────────────────────────────────────────────────────────

// ProjectSlug is the Kubernetes namespace a project's workloads run in.
func (a ClientAPI) ProjectSlug(projectID string) (string, error) {
	projects, err := a.C.ListProjects(a.OrgID)
	if err != nil {
		return "", err
	}
	for _, p := range projects {
		if p.ID == projectID {
			return p.Slug, nil
		}
	}
	return "", fmt.Errorf("project %s is no longer in Meshploy", projectID)
}

// DatabaseSlug is what a managed database's objects are named from, which is
// not the service's own slug: its claim is this name with "-data".
func (a ClientAPI) DatabaseSlug(projectID, serviceID string) (string, error) {
	dc, err := a.C.GetDatabaseConfig(a.OrgID, projectID, serviceID)
	if err != nil {
		return "", err
	}
	if dc.Slug == "" {
		return "", fmt.Errorf("this database has no cluster name yet")
	}
	return dc.Slug, nil
}

// VolumeSlug is the claim a volume's data lives in.
func (a ClientAPI) VolumeSlug(projectID, volumeID string) (string, error) {
	v, err := a.C.GetVolume(a.OrgID, projectID, volumeID)
	if err != nil {
		return "", err
	}
	if v.Slug == "" {
		return "", fmt.Errorf("volume %s has no claim yet", v.Name)
	}
	return v.Slug, nil
}

// ServiceImage is what Meshploy runs this workload from - the registry name,
// where a locally built image was carried into the built-in registry.
func (a ClientAPI) ServiceImage(projectID, serviceID string) (string, error) {
	svc, err := a.C.GetService(a.OrgID, projectID, serviceID)
	if err != nil {
		return "", err
	}
	if svc.Image == "" {
		return "", fmt.Errorf("%s has no image", svc.Name)
	}
	return svc.Image, nil
}

// RunningPod is the pod a service is running right now.
//
// Ready, not merely present: a restore executed in a container that is still
// starting fails in ways that read like a broken dump.
func (a ClientAPI) RunningPod(projectID, serviceID string) (string, error) {
	pods, err := a.C.ListPods(a.OrgID, projectID, serviceID)
	if err != nil {
		return "", err
	}
	for _, p := range pods {
		if p.Ready && p.Phase == "Running" {
			return p.Name, nil
		}
	}
	return "", fmt.Errorf("no ready pod to restore into yet")
}

// HTTPProbe checks a moved domain answers through the edge that is still
// terminating TLS for it - Dokploy's, until cutover.
//
// It asks for the hostname over plain HTTP against the host, because that is
// the path the switch changed: the edge terminates TLS and forwards to
// Meshploy's proxy. Any answer at all is a pass: a 401 or a 302 is the
// application responding, and a migration has no business deciding which status
// codes an app is allowed to return.
//
// The one answer that means nothing is the edge's own redirect to HTTPS. Every
// hostname on a Dokploy server gets one before any backend is consulted, so a
// probe that stopped there passed whatever happened behind it - including a
// move that left the domain answering 502. When the edge redirects to HTTPS,
// the probe follows it back to the same edge over TLS, which is the request
// that actually reaches the application.
type HTTPProbe struct {
	// Addr is where the edge listens, "127.0.0.1:80" unless something else
	// holds the port.
	Addr string
	// TLSAddr is where the same edge terminates TLS. Empty uses Addr's host on
	// port 443.
	TLSAddr string
	Timeout time.Duration
	// Window is how long a domain has to start answering. An edge that has just
	// taken the ports is still opening listeners and may be issuing a
	// certificate, and a proxy that has just been told about a route serves it
	// on its next refresh - so the first request after a change is not the
	// answer, and a single one turned "not yet" into "this move failed".
	Window time.Duration
	// Interval is how often it asks inside that window.
	Interval time.Duration
}

// probeWindow and probeInterval bound the wait: long enough for an edge to come
// up and a route cache to refresh, short enough that a domain which is really
// down is reported while the operator is still watching.
const (
	probeWindow   = 90 * time.Second
	probeInterval = 2 * time.Second
)

func (p HTTPProbe) Probe(hostname string) error {
	window, interval := p.Window, p.Interval
	if window == 0 {
		window = probeWindow
	}
	if interval <= 0 {
		interval = probeInterval
	}
	deadline := time.Now().Add(window)
	for {
		err := p.probeOnce(hostname)
		if err == nil || !time.Now().Before(deadline) {
			return err
		}
		time.Sleep(interval)
	}
}

func (p HTTPProbe) probeOnce(hostname string) error {
	addr := p.Addr
	if addr == "" {
		addr = "127.0.0.1:80"
	}
	timeout := p.Timeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	req, err := http.NewRequest(http.MethodGet, "http://"+addr+"/", nil)
	if err != nil {
		return err
	}
	req.Host = hostname
	resp, err := (&http.Client{
		Timeout: timeout,
		// A redirect to HTTPS is the edge doing its job, not an answer to
		// follow: following it would test DNS and certificates, which have not
		// moved yet.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 500 {
		return fmt.Errorf("the edge answered %s", resp.Status)
	}
	if !redirectsToHTTPS(resp) {
		return nil
	}
	return p.probeTLS(hostname, timeout)
}

// redirectsToHTTPS reports whether this is the edge sending the caller to TLS,
// rather than the application redirecting somewhere of its own.
func redirectsToHTTPS(resp *http.Response) bool {
	if resp.StatusCode < 300 || resp.StatusCode > 399 {
		return false
	}
	return strings.HasPrefix(strings.ToLower(resp.Header.Get("Location")), "https://")
}

// probeTLS asks the same edge for the hostname over TLS.
//
// The certificate is not checked. The connection is to this host, the name is
// carried in SNI so the edge picks the right certificate, and what is being
// tested is whether the application answers - not whether the certificate the
// edge already had is still valid.
func (p HTTPProbe) probeTLS(hostname string, timeout time.Duration) error {
	addr := p.TLSAddr
	if addr == "" {
		host := p.Addr
		if host == "" {
			host = "127.0.0.1:80"
		}
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		addr = net.JoinHostPort(host, "443")
	}
	req, err := http.NewRequest(http.MethodGet, "https://"+addr+"/", nil)
	if err != nil {
		return err
	}
	req.Host = hostname
	resp, err := (&http.Client{
		Timeout: timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Transport: &http.Transport{TLSClientConfig: &tls.Config{
			ServerName:         hostname,
			InsecureSkipVerify: true, //nolint:gosec // testing the app, over loopback
		}},
	}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 500 {
		return fmt.Errorf("the edge answered %s", resp.Status)
	}
	return nil
}

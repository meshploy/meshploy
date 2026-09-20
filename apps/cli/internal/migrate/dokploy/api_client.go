package dokploy

import (
	"fmt"
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

func (a ClientAPI) ServiceStatus(projectID, serviceID string) (string, error) {
	svc, err := a.C.GetService(a.OrgID, projectID, serviceID)
	if err != nil {
		return "", err
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

// HTTPProbe checks a moved domain answers through the edge that is still
// terminating TLS for it - Dokploy's, until cutover.
//
// It asks for the hostname over plain HTTP against the host, because that is
// the path the switch changed: the edge terminates TLS and forwards to
// Meshploy's proxy. Any answer at all is a pass: a 401 or a 302 is the
// application responding, and a migration has no business deciding which status
// codes an app is allowed to return.
type HTTPProbe struct {
	// Addr is where the edge listens, "127.0.0.1:80" unless something else
	// holds the port.
	Addr    string
	Timeout time.Duration
}

func (p HTTPProbe) Probe(hostname string) error {
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
	return nil
}

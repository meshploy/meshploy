package cmd

import (
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"github.com/meshploy/apps/cli/internal/dockerapi"
	"github.com/meshploy/packages/hostagent"
)

// Docker starts containers at boot, often before the mesh has its address. Where
// ip_nonlocal_bind is off, which it is on an install older than the installer's
// setting of it, a port published on that address fails to bind and Docker gives
// up on the container for good, whatever its restart policy: on a gateway that
// is the API and the registry, down until someone notices. So the agent watches
// for it, and once the address is here recreates those services. Recreated and
// not started: a container that failed this way has lost its network, and
// starting it gives a service that runs and reaches nothing.
//
// A container someone stopped carries no such error, so it is never touched.

const (
	recoverInterval = 15 * time.Second
	// recoverRetry spaces attempts at a recreate that did not fix it.
	recoverRetry = 2 * time.Minute

	composeProjectLabel = "com.docker.compose.project"
	composeServiceLabel = "com.docker.compose.service"
)

// bindFailed reports whether a container never started because a published
// port could not be bound.
func bindFailed(d dockerapi.ContainerDetail) bool {
	if d.State.Status == "running" || d.State.Status == "restarting" {
		return false
	}
	return strings.Contains(d.State.Error, "failed to bind host port") ||
		strings.Contains(d.State.Error, "cannot assign requested address")
}

// stuckServices are the compose services of project in that state.
func stuckServices(ctx context.Context, project string) ([]string, error) {
	c, err := dockerapi.Open(ctx)
	if err != nil {
		return nil, err
	}
	all, err := c.Containers(ctx)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, s := range all {
		if s.State == "running" {
			continue
		}
		d, err := c.Inspect(ctx, s.ID)
		if err != nil {
			continue
		}
		if d.Config.Labels[composeProjectLabel] == project && bindFailed(d) {
			out = append(out, d.Config.Labels[composeServiceLabel])
		}
	}
	return out, nil
}

// hasAddress reports whether this machine holds ip.
func hasAddress(ip string) bool {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return false
	}
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok && n.IP.String() == ip {
			return true
		}
	}
	return false
}

// recoverer is what it takes to bring services back, with its seams for tests.
type recoverer struct {
	logw     io.Writer
	stuck    func(context.Context) ([]string, error)
	meshUp   func() bool
	recreate func(services []string) error
	lastTry  time.Time
}

func newRecoverer(logw io.Writer) *recoverer {
	return &recoverer{
		logw:   logw,
		stuck:  func(ctx context.Context) ([]string, error) { return stuckServices(ctx, "meshploy") },
		meshUp: func() bool { ip := readEnvVar("MESH_IP"); return ip == "" || hasAddress(ip) },
		recreate: func(services []string) error {
			args := append([]string{"up", "-d", "--pull", "never", "--no-deps", "--force-recreate"}, services...)
			return composeRun(detectContainerRuntime(), args...)
		},
	}
}

// check brings back whatever is stuck, reporting whether it tried.
func (r *recoverer) check(ctx context.Context, now time.Time) (hostagent.Task, bool) {
	stuck, err := r.stuck(ctx)
	if err != nil || len(stuck) == 0 {
		// A runtime still starting is not news: the next look finds it.
		return hostagent.Task{}, false
	}
	if !r.meshUp() || now.Sub(r.lastTry) < recoverRetry {
		return hostagent.Task{}, false
	}
	r.lastTry = now
	names := strings.Join(stuck, ", ")
	fmt.Fprintf(r.logw, "recovery: %s could not bind their ports at boot: recreating them\n", names)
	if err := r.recreate(stuck); err != nil {
		fmt.Fprintf(r.logw, "recovery: recreating %s: %v\n", names, err)
		return hostagent.Task{Error: fmt.Sprintf("recreating %s: %v", names, err), At: now.UTC()}, true
	}
	fmt.Fprintf(r.logw, "recovery: %s recreated\n", names)
	return hostagent.Task{OK: true, At: now.UTC()}, true
}

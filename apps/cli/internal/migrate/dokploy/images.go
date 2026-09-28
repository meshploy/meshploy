package dokploy

import (
	"fmt"
	"strings"

	"github.com/meshploy/apps/cli/internal/migrate"
	"github.com/meshploy/apps/cli/internal/migrate/journal"
)

// Carrying images across.
//
// A workload migrates with the image it is running now, not the latest
// successful build: in Dokploy those differ after a rollback, and what is
// running is the truth. But an image Dokploy built on this host exists nowhere
// else - k3s, pulling for the new copy, would find nothing. So it is pushed
// into Meshploy's built-in registry first.
//
// An image that came from a registry is left alone. Copying `postgres:16` into
// our own registry would cost disk and buy nothing: the cluster can pull it.
//
// The test for "only exists here" is the image's RepoDigests. An image pulled
// from a registry carries the digest it was pulled by; one built locally has
// none. The name cannot be used for this - a Dokploy build is often called
// `myapp:latest`, which parses as a Docker Hub reference that does not exist.

// Registry is Meshploy's built-in registry, as reachable from this host.
type Registry struct {
	// Endpoint is host:port, e.g. "100.64.0.1:5000": what the cluster pulls.
	Endpoint string
	// PushVia is the same registry by another address, the one pushed to:
	// "127.0.0.1:5000" on the gateway it runs on. Empty pushes to Endpoint.
	PushVia string
}

// pushRef is ref named by PushVia, which is where it is pushed.
func (r Registry) pushRef(ref string) string {
	if r.PushVia == "" || !strings.HasPrefix(ref, r.Endpoint+"/") {
		return ref
	}
	return r.PushVia + strings.TrimPrefix(ref, r.Endpoint)
}

// ImageMover copies locally built images into the built-in registry.
type ImageMover struct {
	Runner   migrate.Runner
	Registry Registry
	Journal  *journal.Journal
}

// NeedsPush reports whether this image exists only on this host.
//
// Errors are treated as "needs pushing": an image Docker cannot describe is one
// we should carry rather than hope the cluster finds.
func (m ImageMover) NeedsPush(image string) bool {
	if image == "" {
		return false
	}
	out, err := m.Runner.Output("docker", "image", "inspect", image, "--format", "{{len .RepoDigests}}")
	if err != nil {
		return true
	}
	return strings.TrimSpace(out) == "0"
}

// Push tags an image into the built-in registry and pushes it, returning the
// reference the cluster should pull.
//
// An image that came from a registry is returned unchanged, so callers can pass
// everything through this and let it decide.
func (m ImageMover) Push(step, group, image string) (string, error) {
	if image == "" || m.Registry.Endpoint == "" {
		return image, nil
	}
	if !m.NeedsPush(image) {
		return image, nil
	}
	target := m.Registry.Ref(image)

	if m.Journal != nil && m.Journal.Done(step) {
		return target, nil
	}
	entry := journal.Entry{Step: step, Group: group, Action: "push-image", Target: target, Result: journal.OK, Created: target}
	push := m.Registry.pushRef(target)
	if _, err := m.Runner.Output("docker", "tag", image, push); err != nil {
		return "", m.fail(entry, fmt.Errorf("tag %s: %w", image, err))
	}
	if _, err := m.Runner.Output("docker", "push", push); err != nil {
		return "", m.fail(entry, fmt.Errorf("push %s: %w", push, err))
	}
	if m.Journal != nil {
		// No undo: an image in a registry harms nothing, and deleting one a
		// running workload pulls from would be worse than leaving it. Finish
		// cleans the registry, not rollback.
		_ = m.Journal.Append(entry)
	}
	return target, nil
}

// Ref is where an image lands in the built-in registry.
//
// The original repository and tag are kept under the registry's host so the
// name still says what it is - `100.64.0.1:5000/myapp:latest` rather than a
// hash nobody can read. A reference that already names a registry host has that
// host replaced, not appended.
func (r Registry) Ref(image string) string {
	name, tag := image, "latest"
	if i := strings.LastIndex(image, ":"); i > strings.LastIndex(image, "/") {
		name, tag = image[:i], image[i+1:]
	}
	// Drop a registry host if the name carries one: a host is the part before
	// the first slash that looks like an address.
	if i := strings.Index(name, "/"); i > 0 {
		head := name[:i]
		if strings.ContainsAny(head, ".:") || head == "localhost" {
			name = name[i+1:]
		}
	}
	return r.Endpoint + "/" + name + ":" + tag
}

func (m ImageMover) fail(e journal.Entry, cause error) error {
	e.Result, e.Error, e.Created = journal.Failed, cause.Error(), ""
	if m.Journal != nil {
		_ = m.Journal.Append(e)
	}
	return cause
}

// Carry makes the image a container is running pullable by the cluster, pinned
// to exactly that image, and returns the reference.
//
// It reads the image the container runs by ID, not by its tag: a tag can have
// moved on since the container started - a newer build not yet deployed, a
// re-pulled `latest` - and a migration moves what is running. An image that
// came from a registry is referred to by its digest, so a floating tag such as
// `timescale/timescaledb:latest-pg16` cannot hand the moved database a newer
// engine than its data was written by. One built on this host is pushed into
// the built-in registry under a tag made from its ID.
func (m ImageMover) Carry(step, group, container string) (string, error) {
	out, err := m.Runner.Output("docker", "inspect", container, "--format", "{{.Image}}|{{.Config.Image}}")
	if err != nil {
		return "", fmt.Errorf("read the image %s runs: %w", container, err)
	}
	id, name, _ := strings.Cut(strings.TrimSpace(out), "|")
	digests, err := m.Runner.Output("docker", "image", "inspect", id, "--format", "{{range .RepoDigests}}{{.}} {{end}}")
	if err != nil {
		return "", fmt.Errorf("read image %s: %w", id, err)
	}
	// A registry image is referred to by its digest - unless the registry no
	// longer has it. Images disappear upstream: MinIO stopped publishing
	// minio/minio, and a server that pulled it years ago is the only place
	// left holding it. The cluster could not pull that, so it is carried like
	// one built here.
	if fields := strings.Fields(digests); len(fields) > 0 {
		if _, err := m.Runner.Output("docker", "manifest", "inspect", fields[0]); err == nil {
			return fields[0], nil
		}
	}
	if m.Registry.Endpoint == "" {
		return "", fmt.Errorf("%s runs an image built on this host, and there is no registry to carry it to", container)
	}
	short := strings.TrimPrefix(id, "sha256:")
	if len(short) > 12 {
		short = short[:12]
	}
	repo := m.Registry.Ref(name)
	repo = repo[:strings.LastIndex(repo, ":")]
	target := repo + ":dokploy-" + short

	if m.Journal != nil && m.Journal.Done(step) {
		return target, nil
	}
	entry := journal.Entry{Step: step, Group: group, Action: "push-image", Target: target, Result: journal.OK, Created: target}
	push := m.Registry.pushRef(target)
	if _, err := m.Runner.Output("docker", "tag", id, push); err != nil {
		return "", m.fail(entry, fmt.Errorf("tag %s: %w", id, err))
	}
	if _, err := m.Runner.Output("docker", "push", push); err != nil {
		return "", m.fail(entry, fmt.Errorf("push %s: %w", push, err))
	}
	if m.Journal != nil {
		_ = m.Journal.Append(entry)
	}
	return target, nil
}

// ComposeContainer is the container a compose project runs for one of its
// services, stopped ones included: a one-shot service's finished container
// still says which image it ran.
func (m ImageMover) ComposeContainer(project, service string) (string, error) {
	out, err := m.Runner.Output("docker", "ps", "-a",
		"--filter", "label=com.docker.compose.project="+project,
		"--filter", "label=com.docker.compose.service="+service,
		"--format", "{{.ID}}")
	if err != nil {
		return "", err
	}
	if ids := strings.Fields(out); len(ids) > 0 {
		return ids[0], nil
	}
	return "", nil
}

package dokploy

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/meshploy/apps/cli/internal/migrate"
	"github.com/meshploy/apps/cli/internal/migrate/journal"
)

// Stopping and starting Dokploy's own copies.
//
// Stopping is the strongest thing a migration does to Dokploy before finish:
// nothing is deleted, so every step here has an undo that puts the workload
// back exactly as it was. A Swarm service is scaled to zero and its replica
// count recorded; a plain container is stopped by name.
//
// Reading the current state before changing it is the point. "Scale back to 1"
// is a guess; "scale back to what it was" is a rollback.

// Workload is one thing to stop or start, as Docker sees it.
type Workload struct {
	// Name is the Swarm service or container name.
	Name string
	// Swarm is true for a Swarm service, false for a plain container.
	Swarm bool
}

// Control stops and starts Dokploy workloads, writing what it did to the
// journal as it goes.
type Control struct {
	Runner  migrate.Runner
	Journal *journal.Journal
}

// Stop takes a workload out of service and records how to bring it back.
//
// Idempotent through the journal: a step already done is not repeated, which is
// what makes a failed move resumable.
func (c Control) Stop(step, group string, w Workload) error {
	if c.Journal != nil && c.Journal.Done(step) {
		return nil
	}
	entry := journal.Entry{Step: step, Group: group, Target: w.Name, Result: journal.OK}

	if w.Swarm {
		replicas, err := c.replicas(w.Name)
		if err != nil {
			return c.fail(entry, "scale-service", err)
		}
		entry.Action = "scale-service"
		if replicas == 0 {
			// Already down. Recorded as skipped so a rollback does not start
			// something the operator had stopped themselves.
			entry.Result = journal.Skipped
			return c.append(entry)
		}
		if _, err := c.Runner.Output("docker", "service", "scale", "--detach", fmt.Sprintf("%s=0", w.Name)); err != nil {
			return c.fail(entry, "scale-service", err)
		}
		entry.Undo = &journal.Undo{Kind: journal.UndoScaleService, Args: map[string]string{
			"service": w.Name, "replicas": strconv.Itoa(replicas),
		}}
		return c.append(entry)
	}

	entry.Action = "stop-container"
	running, err := c.running(w.Name)
	if err != nil {
		return c.fail(entry, "stop-container", err)
	}
	if !running {
		entry.Result = journal.Skipped
		return c.append(entry)
	}
	// Stopping is not enough for a container Docker was told to always restart.
	// Dokploy's Traefik is one, so after a reboot it came back and raced
	// Meshploy's edge for 80 and 443 - and whichever won, one of them was
	// serving every domain on the server. So the policy is recorded and
	// cleared before the stop, and put back by the undo.
	policy, err := c.restartPolicy(w.Name)
	if err != nil {
		return c.fail(entry, "stop-container", err)
	}
	undoArgs := map[string]string{"container": w.Name}
	if policy != "" && policy != "no" {
		if _, err := c.Runner.Output("docker", "update", "--restart=no", w.Name); err != nil {
			return c.fail(entry, "stop-container", err)
		}
		undoArgs["restart"] = policy
	}
	if _, err := c.Runner.Output("docker", "stop", w.Name); err != nil {
		return c.fail(entry, "stop-container", err)
	}
	entry.Undo = &journal.Undo{Kind: journal.UndoStartContainer, Args: undoArgs}
	return c.append(entry)
}

// restartPolicy is what Docker does with this container when the daemon starts.
// Empty where the container does not say, which is the same as "no".
func (c Control) restartPolicy(name string) (string, error) {
	out, err := c.Runner.Output("docker", "inspect", name, "--format", "{{.HostConfig.RestartPolicy.Name}}")
	if err != nil {
		return "", fmt.Errorf("read %s restart policy: %w", name, err)
	}
	return strings.TrimSpace(out), nil
}

// Start puts a workload back, for a rollback that is not replaying the journal
// - a move that failed halfway and is undoing its own steps.
func (c Control) Start(w Workload, replicas int) error {
	if w.Swarm {
		if replicas < 1 {
			replicas = 1
		}
		_, err := c.Runner.Output("docker", "service", "scale", "--detach", fmt.Sprintf("%s=%d", w.Name, replicas))
		return err
	}
	_, err := c.Runner.Output("docker", "start", w.Name)
	return err
}

// replicas is a Swarm service's desired count, which is what a rollback has to
// restore. A service Docker does not know is an error: scaling something that
// is not there would silently succeed at nothing.
func (c Control) replicas(name string) (int, error) {
	out, err := c.Runner.Output("docker", "service", "inspect", name, "--format", "{{.Spec.Mode.Replicated.Replicas}}")
	if err != nil {
		return 0, fmt.Errorf("read %s: %w", name, err)
	}
	n, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return 0, fmt.Errorf("read %s replicas: %q", name, strings.TrimSpace(out))
	}
	return n, nil
}

func (c Control) running(name string) (bool, error) {
	out, err := c.Runner.Output("docker", "inspect", name, "--format", "{{.State.Running}}")
	if err != nil {
		return false, fmt.Errorf("read %s: %w", name, err)
	}
	return strings.TrimSpace(out) == "true", nil
}

func (c Control) append(e journal.Entry) error {
	if c.Journal == nil {
		return nil
	}
	return c.Journal.Append(e)
}

func (c Control) fail(e journal.Entry, action string, cause error) error {
	e.Action, e.Result, e.Error = action, journal.Failed, cause.Error()
	_ = c.append(e)
	return fmt.Errorf("%s %s: %w", action, e.Target, cause)
}

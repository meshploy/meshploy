package dokploy

import (
	"fmt"
	"testing"
	"time"
)

// flakyPortRunner refuses to start a container until a port frees up.
type flakyPortRunner struct {
	fakeRunner
	busy int
}

func (f *flakyPortRunner) Output(name string, args ...string) (string, error) {
	if name == "docker" && len(args) > 0 && args[0] == "start" && f.busy > 0 {
		f.busy--
		f.ran = append(f.ran, "docker start (busy)")
		return "", fmt.Errorf("failed to bind host port 127.0.0.1:3100/tcp: address already in use")
	}
	return f.fakeRunner.Output(name, args...)
}

// A container put back waits for the host port Meshploy's gateway is still
// letting go of, rather than failing the rollback while the moved copy's
// paused route closes.
func TestAContainerPutBackWaitsForItsPort(t *testing.T) {
	runner := &flakyPortRunner{fakeRunner: fakeRunner{replies: map[string]string{}}, busy: 2}
	r := Rollback{Runner: runner, Sleep: func(time.Duration) {}}
	if err := r.startContainer("pi-ui"); err != nil {
		t.Fatalf("%v (%v)", err, runner.ran)
	}
	if !runner.didRun("docker start pi-ui") {
		t.Errorf("never started: %v", runner.ran)
	}
}

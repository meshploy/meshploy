package dockerapi

import "testing"

// DOCKER_HOST names the runtime to talk to, and searching on past it would
// report another daemon's containers as this host's. It is also what makes
// "no runtime here" testable: without this, the test that asserts an empty
// report passed only on a machine that happened to have no Docker, and failed
// on every CI runner, which has one.
func TestDockerHostIsTheAnswerNotOneMoreCandidate(t *testing.T) {
	t.Setenv("DOCKER_HOST", "unix:///tmp/chosen.sock")
	if got := Sockets(); len(got) != 1 || got[0] != "/tmp/chosen.sock" {
		t.Errorf("Sockets() = %v, want only the socket asked for", got)
	}

	// Unset, the usual search runs.
	t.Setenv("DOCKER_HOST", "")
	if got := Sockets(); len(got) < 3 || got[0] != "/var/run/docker.sock" {
		t.Errorf("Sockets() = %v, want the default search", got)
	}

	// A scheme this does not speak is not an answer either.
	t.Setenv("DOCKER_HOST", "tcp://10.0.0.1:2375")
	if got := Sockets(); len(got) < 3 {
		t.Errorf("Sockets() = %v, want the default search", got)
	}
}

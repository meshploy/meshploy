package migrate

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Reaching into the cluster from the host.
//
// A migration runs on the gateway as root, beside the cluster rather than
// inside it, and the API it talks to is an HTTP API: right for creating a
// service, useless for piping eight gigabytes into a database. So the data
// step goes through kubectl, the same way it goes through docker for the side
// it is copying from - and for a workload placed on another node, kubectl is
// also what makes the destination not matter.

// Kube runs commands in the cluster.
type Kube struct {
	// Bin and Args are the command, found by Find. "k3s kubectl" on a Meshploy
	// gateway, plain "kubectl" where someone has one.
	Bin  string
	Args []string
	// Kubeconfig is set in the environment for each call. Empty uses whatever
	// the command finds for itself.
	Kubeconfig string
	Stream     Streamer
}

// FindKube looks for a way to reach the cluster on this host.
func FindKube(stream Streamer) (Kube, error) {
	if stream == nil {
		stream = ExecStreamer{}
	}
	// k3s first: on a Meshploy gateway the cluster is k3s, and its bundled
	// kubectl is guaranteed to match it. A standalone kubectl may be anyone's
	// and point anywhere.
	if _, err := os.Stat("/usr/local/bin/k3s"); err == nil {
		return Kube{Bin: "/usr/local/bin/k3s", Args: []string{"kubectl"},
			Kubeconfig: "/etc/rancher/k3s/k3s.yaml", Stream: stream}, nil
	}
	if path, err := exec.LookPath("kubectl"); err == nil {
		return Kube{Bin: path, Stream: stream}, nil
	}
	return Kube{}, fmt.Errorf("no kubectl on this host: the data step needs one to reach the cluster")
}

// Run calls kubectl with stdin and stdout attached.
func (k Kube) Run(stdin io.Reader, stdout io.Writer, args ...string) error {
	if k.Bin == "" {
		return fmt.Errorf("no kubectl was found on this host")
	}
	if k.Kubeconfig != "" {
		// Set for the child only: the migration must not change the
		// environment of anything else running in this process.
		old, had := os.LookupEnv("KUBECONFIG")
		_ = os.Setenv("KUBECONFIG", k.Kubeconfig)
		defer func() {
			if had {
				_ = os.Setenv("KUBECONFIG", old)
				return
			}
			_ = os.Unsetenv("KUBECONFIG")
		}()
	}
	return k.Stream.Stream(stdin, stdout, k.Bin, append(append([]string{}, k.Args...), args...)...)
}

// Out runs kubectl and returns its output, trimmed.
func (k Kube) Out(args ...string) (string, error) {
	var buf strings.Builder
	if err := k.Run(nil, &buf, args...); err != nil {
		return "", err
	}
	return strings.TrimSpace(buf.String()), nil
}

// Exec runs a command inside a pod's first container.
func (k Kube) Exec(namespace, pod string, stdin io.Reader, stdout io.Writer, cmd ...string) error {
	args := []string{"exec", "-n", namespace}
	if stdin != nil {
		args = append(args, "-i")
	}
	args = append(args, pod, "--")
	return k.Run(stdin, stdout, append(args, cmd...)...)
}

// Apply sends a manifest to the cluster.
func (k Kube) Apply(manifest string) error {
	return k.Run(strings.NewReader(manifest), nil, "apply", "-f", "-")
}

// WaitReady waits for a pod to be ready, which is the only way to know the
// container is there to be executed in.
func (k Kube) WaitReady(namespace, pod string, timeout time.Duration) error {
	_, err := k.Out("wait", "-n", namespace, "--for=condition=Ready",
		"pod/"+pod, fmt.Sprintf("--timeout=%ds", int(timeout.Seconds())))
	return err
}

// DeletePod removes a pod and waits for it to go, so a re-run does not meet
// the one it made last time.
func (k Kube) DeletePod(namespace, pod string) error {
	_, err := k.Out("delete", "pod", "-n", namespace, pod, "--ignore-not-found", "--wait=true", "--timeout=120s")
	return err
}

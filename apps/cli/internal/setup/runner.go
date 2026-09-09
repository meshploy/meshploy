package setup

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

// ScriptRunner drives the existing installer rather than reimplementing it.
//
// install.sh's `ask` reads each answer from the environment under --auto, so
// the browser path and the terminal path run the same script with the same
// arguments. That is the point: a second implementation of "install Meshploy"
// would drift from this one within a release.
type ScriptRunner struct {
	Script string   // path to install.sh
	Dir    string   // working directory, normally /opt/meshploy
	Extra  []string // additional KEY=VALUE, mainly for tests
}

// ansi matches the colour escapes install.sh writes. They are meant for a
// terminal; rendered in a browser they are noise sitting in front of the text
// the operator is trying to read.
var ansi = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

func (s ScriptRunner) Run(ctx context.Context, a Answers, out func(string)) error {
	args := []string{s.Script, "--auto"}
	if a.DNSMode != "" {
		args = append(args, "--dns-mode="+a.DNSMode)
	}

	cmd := exec.CommandContext(ctx, "bash", args...)
	cmd.Dir = s.Dir
	cmd.Env = append(os.Environ(),
		// NODE_TYPE has to be explicit: without it --auto has no way to know
		// which half of the script to run, and asks.
		"NODE_TYPE=master",
		"DOMAIN="+a.Domain,
		"PUBLIC_IP="+a.PublicIP,
	)
	if a.MeshIP != "" {
		cmd.Env = append(cmd.Env, "MESH_IP="+a.MeshIP)
	}
	// POSTGRES_PASSWORD is deliberately not set. ask_optional leaves it empty
	// under --auto and install.sh generates one, which is better than carrying a
	// password across the plaintext connection this wizard is served over.
	cmd.Env = append(cmd.Env, s.Extra...)

	// Merged, because the operator wants one transcript in the order it
	// happened, not stdout and stderr interleaved by luck.
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("pipe installer output: %w", err)
	}
	cmd.Stderr = cmd.Stdout.(io.Writer)

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start installer: %w", err)
	}

	sc := bufio.NewScanner(pipe)
	// install.sh prints progress with \r and long summaries; the default 64KB
	// token limit is enough, but a runaway line should not kill the stream.
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimRight(ansi.ReplaceAllString(sc.Text(), ""), "\r ")
		if line != "" {
			out(line)
		}
	}

	if err := cmd.Wait(); err != nil {
		// The transcript already carries the reason; this is the summary the
		// wizard shows beside it.
		var code int
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		}
		if code != 0 {
			return fmt.Errorf("the installer exited with status %d — see the log above", code)
		}
		return fmt.Errorf("installer: %w", err)
	}
	return nil
}

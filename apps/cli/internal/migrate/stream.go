package migrate

import (
	"bytes"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// Streaming a command's input and output, for the parts of a migration that
// move bytes rather than read state.
//
// Runner returns a string, which is right for "what replicas does this service
// have" and wrong for a database dump: a dump is piped from one process into
// another and never has to be a value in this program's memory. Everything a
// migration copies goes through here.

// Streamer runs a command with its standard input and output attached.
type Streamer interface {
	// Stream runs name with args, reading stdin (nil for none) and writing
	// stdout (nil to discard). The error carries the command's stderr, because
	// a failed dump says why on stderr and nowhere else.
	Stream(stdin io.Reader, stdout io.Writer, name string, args ...string) error
}

// ExecStreamer streams commands on this host.
type ExecStreamer struct{}

func (ExecStreamer) Stream(stdin io.Reader, stdout io.Writer, name string, args ...string) error {
	var stderr bytes.Buffer
	c := exec.Command(name, args...)
	c.Stdin, c.Stdout, c.Stderr = stdin, stdout, &stderr
	if c.Stdout == nil {
		c.Stdout = io.Discard
	}
	if err := c.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return fmt.Errorf("%s: %w: %s", name, err, lastLine(msg))
		}
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

// lastLine is the end of a command's stderr, which is where the reason is: a
// dump tool prints its progress first and its complaint last.
func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

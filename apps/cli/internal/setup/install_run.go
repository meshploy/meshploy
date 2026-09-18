package setup

import "sync"

// installRun is one install, and everyone watching it.
//
// It exists because an install must not belong to the HTTP request that
// started it: it writes /opt/meshploy/.env, generates the CoreDNS, Headscale
// and Caddy configuration and drives compose, so a closed tab or a dropped
// connection part way through leaves a machine half installed. The run keeps
// going; browsers attach to it and fall off it.
type installRun struct {
	mu      sync.Mutex
	lines   []string
	subs    []chan string
	done    chan struct{}
	err     error
	stopped bool
}

// newInstallRun starts from what is already on disk, so a retry shows the
// previous attempt above the new one rather than starting on a blank pane.
func newInstallRun(transcript []string) *installRun {
	return &installRun{
		lines: append([]string(nil), transcript...),
		done:  make(chan struct{}),
	}
}

func (r *installRun) append(line string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lines = append(r.lines, line)
	for _, c := range r.subs {
		select {
		case c <- line:
		default:
			// A watcher too slow to keep up loses lines from its live stream,
			// never the run and never the transcript: that is on disk, and a
			// reload replays it whole.
		}
	}
}

// watch returns everything so far and a channel of what comes next, taken
// together so nothing slips between the two.
func (r *installRun) watch() ([]string, chan string, func()) {
	r.mu.Lock()
	defer r.mu.Unlock()
	backlog := append([]string(nil), r.lines...)
	// Generous: an installer's output arrives in bursts, and a browser reading
	// the stream is not always scheduled between them.
	ch := make(chan string, 1024)
	r.subs = append(r.subs, ch)
	return backlog, ch, func() { r.unwatch(ch) }
}

func (r *installRun) unwatch(ch chan string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, c := range r.subs {
		if c == ch {
			r.subs = append(r.subs[:i], r.subs[i+1:]...)
			return
		}
	}
}

// watchers is how many browsers are on this run right now.
func (r *installRun) watchers() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.subs)
}

func (r *installRun) finish(err error) {
	r.mu.Lock()
	if r.stopped {
		r.mu.Unlock()
		return
	}
	r.stopped, r.err = true, err
	r.mu.Unlock()
	close(r.done)
}

func (r *installRun) finished() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.stopped
}

func (r *installRun) failure() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.err
}

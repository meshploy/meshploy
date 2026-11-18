package k8s

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"
)

// agentDialTimeout bounds connecting to a cluster agent. The agent is a mesh
// peer, which answers in milliseconds when it is there; one that has left the
// mesh never answers, and the default would wait half a minute for it.
const agentDialTimeout = 5 * time.Second

// agentDownFor is how long a failed connection stands for the agent being
// away: calls in that time fail at once, so a page asking the cluster many
// things waits once rather than once per question.
const agentDownFor = 10 * time.Second

// agentDialer connects to a cluster agent, failing fast while the last attempt
// could not reach it.
type agentDialer struct {
	dial func(ctx context.Context, network, addr string) (net.Conn, error)
	now  func() time.Time

	mu     sync.Mutex
	down   error
	downAt time.Time
}

func newAgentDialer() *agentDialer {
	d := &net.Dialer{Timeout: agentDialTimeout, KeepAlive: 30 * time.Second}
	return &agentDialer{dial: d.DialContext, now: time.Now}
}

func (a *agentDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	a.mu.Lock()
	if a.down != nil && a.now().Sub(a.downAt) < agentDownFor {
		err := a.down
		a.mu.Unlock()
		return nil, err
	}
	a.mu.Unlock()

	conn, err := a.dial(ctx, network, addr)

	a.mu.Lock()
	defer a.mu.Unlock()
	if err != nil {
		// A caller giving up says nothing about the agent.
		if ctx.Err() == nil {
			a.down = fmt.Errorf("the cluster agent cannot be reached: %w", err)
			a.downAt = a.now()
		}
		return nil, err
	}
	a.down = nil
	return conn, nil
}

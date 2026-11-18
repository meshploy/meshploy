package k8s

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

// An agent that could not be reached is not dialled again for a while, and
// one reached again clears that at once.
func TestAgentDialerFailsFastWhileTheAgentIsAway(t *testing.T) {
	now := time.Unix(0, 0)
	dials := 0
	reachable := false
	a := &agentDialer{
		now: func() time.Time { return now },
		dial: func(ctx context.Context, network, addr string) (net.Conn, error) {
			dials++
			if !reachable {
				return nil, errors.New("i/o timeout")
			}
			c, _ := net.Pipe()
			return c, nil
		},
	}
	ctx := context.Background()

	if _, err := a.DialContext(ctx, "tcp", "agent:7443"); err == nil {
		t.Fatal("an unreachable agent connected")
	}
	if _, err := a.DialContext(ctx, "tcp", "agent:7443"); err == nil || dials != 1 {
		t.Fatalf("within the window: err %v after %d dials, want an error after 1", err, dials)
	}

	now = now.Add(agentDownFor)
	reachable = true
	if _, err := a.DialContext(ctx, "tcp", "agent:7443"); err != nil || dials != 2 {
		t.Fatalf("after the window: err %v after %d dials, want a connection after 2", err, dials)
	}
	if _, err := a.DialContext(ctx, "tcp", "agent:7443"); err != nil || dials != 3 {
		t.Fatalf("reached again: err %v after %d dials, want a connection after 3", err, dials)
	}
}

// A caller that gave up does not mark the agent as away.
func TestAgentDialerIgnoresACancelledCaller(t *testing.T) {
	dials := 0
	a := &agentDialer{
		now: time.Now,
		dial: func(ctx context.Context, network, addr string) (net.Conn, error) {
			dials++
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			c, _ := net.Pipe()
			return c, nil
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := a.DialContext(ctx, "tcp", "agent:7443"); err == nil {
		t.Fatal("a cancelled dial connected")
	}
	if _, err := a.DialContext(context.Background(), "tcp", "agent:7443"); err != nil || dials != 2 {
		t.Fatalf("err %v after %d dials, want a connection after 2", err, dials)
	}
}

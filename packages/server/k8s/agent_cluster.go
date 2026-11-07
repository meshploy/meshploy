package k8s

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// AgentIdentity is how the API reaches a cluster agent: its mesh address
// (host:port), the certificate it is pinned to, and the token presented to it.
type AgentIdentity struct {
	Addr    string
	CertPEM []byte
	Token   string
}

func (a AgentIdentity) equal(b AgentIdentity) bool {
	return a.Addr == b.Addr && a.Token == b.Token && bytes.Equal(a.CertPEM, b.CertPEM)
}

// ErrNoClusterAgent is cluster work asked of an API that reaches its cluster
// only through a cluster agent, while none is registered.
var ErrNoClusterAgent = errors.New("no cluster agent is registered; this API reaches its cluster only through one")

// AgentCluster is the cluster of an API that reaches it through a cluster
// agent which may register, move or be revoked while the API runs.
//
// Its client is made once and kept by every service; each request goes to the
// agent of the moment, with that agent's pinned certificate and token. When
// the agent changes, requests still open to the old one (watches, log
// streams) are cancelled, so nothing keeps talking to an agent that is no
// longer trusted. Without an agent, every request fails with
// ErrNoClusterAgent: there is no other way to the cluster to fall back on.
type AgentCluster struct {
	mu  sync.RWMutex
	cur *agentConn

	cs  kubernetes.Interface
	cfg *rest.Config
}

type agentConn struct {
	id     AgentIdentity
	driver *AgentDriver
	rt     http.RoundTripper
	ctx    context.Context
	cancel context.CancelFunc
}

// agentPlaceholderHost is the host the stable client is made with; every
// request is sent to the current agent's address instead.
const agentPlaceholderHost = "https://" + ClusterAgentServerName

// NewAgentCluster is a cluster with no agent yet.
func NewAgentCluster() (*AgentCluster, error) {
	c := &AgentCluster{}
	c.cfg = &rest.Config{Host: agentPlaceholderHost, Transport: c}
	cs, err := kubernetes.NewForConfig(c.cfg)
	if err != nil {
		return nil, err
	}
	c.cs = cs
	return c, nil
}

// Set makes id the agent requests go to. The same identity again changes
// nothing; a different one cancels what is still open to the old.
func (c *AgentCluster) Set(id AgentIdentity) error {
	c.mu.RLock()
	same := c.cur != nil && c.cur.id.equal(id)
	c.mu.RUnlock()
	if same {
		return nil
	}
	d, err := NewAgentDriver(id.Addr, id.CertPEM, id.Token)
	if err != nil {
		return err
	}
	rt, err := rest.TransportFor(d.cfg)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	next := &agentConn{id: id, driver: d, rt: rt, ctx: ctx, cancel: cancel}
	c.mu.Lock()
	old := c.cur
	c.cur = next
	c.mu.Unlock()
	old.close()
	return nil
}

// Clear revokes the agent: nothing reaches the cluster until another is set.
func (c *AgentCluster) Clear() {
	c.mu.Lock()
	old := c.cur
	c.cur = nil
	c.mu.Unlock()
	old.close()
}

// Current is the agent requests go to, if any.
func (c *AgentCluster) Current() (AgentIdentity, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.cur == nil {
		return AgentIdentity{}, false
	}
	return c.cur.id, true
}

func (a *agentConn) close() {
	if a == nil {
		return
	}
	a.cancel()
	if ci, ok := a.rt.(interface{ CloseIdleConnections() }); ok {
		ci.CloseIdleConnections()
	}
}

func (c *AgentCluster) conn() (*agentConn, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.cur == nil {
		return nil, ErrNoClusterAgent
	}
	return c.cur, nil
}

// RoundTrip sends a request of the stable client to the current agent, bound
// to that agent: it is cancelled if the agent changes before it ends.
func (c *AgentCluster) RoundTrip(req *http.Request) (*http.Response, error) {
	a, err := c.conn()
	if err != nil {
		if req.Body != nil {
			req.Body.Close()
		}
		return nil, err
	}
	ctx, cancel := context.WithCancel(req.Context())
	stop := context.AfterFunc(a.ctx, cancel)
	out := req.Clone(ctx)
	out.URL.Scheme = "https"
	out.URL.Host = a.id.Addr
	out.Host = ""
	resp, err := a.rt.RoundTrip(out)
	if err != nil {
		stop()
		cancel()
		return nil, err
	}
	resp.Body = &boundBody{ReadCloser: resp.Body, done: func() { stop(); cancel() }}
	return resp, nil
}

// boundBody releases a request's binding to its agent once the body is
// closed.
type boundBody struct {
	io.ReadCloser
	once sync.Once
	done func()
}

func (b *boundBody) Close() error {
	err := b.ReadCloser.Close()
	b.once.Do(b.done)
	return err
}

func (c *AgentCluster) Client() kubernetes.Interface { return c.cs }

// RestConfig is the current agent's connection, for exec, port-forward and
// log streams, which make their own connections: a stream opened before the
// agent changes ends when its caller closes it. Without an agent it is a
// configuration that reaches nothing.
func (c *AgentCluster) RestConfig() *rest.Config {
	a, err := c.conn()
	if err != nil {
		return &rest.Config{Host: "https://no-cluster-agent.invalid"}
	}
	return rest.CopyConfig(a.driver.cfg)
}

func (c *AgentCluster) StartBuild(ctx context.Context, p BuildJobParams) error {
	a, err := c.conn()
	if err != nil {
		return err
	}
	return a.driver.StartBuild(ctx, p)
}

func (c *AgentCluster) PruneNodeImages(ctx context.Context, node string, timeout time.Duration) (int, error) {
	a, err := c.conn()
	if err != nil {
		return 0, err
	}
	return a.driver.PruneNodeImages(ctx, node, timeout)
}

func (c *AgentCluster) OpenNodeShell(ctx context.Context, node, podName string) (*corev1.Pod, error) {
	a, err := c.conn()
	if err != nil {
		return nil, err
	}
	return a.driver.OpenNodeShell(ctx, node, podName)
}

func (c *AgentCluster) CloseNodeShell(ctx context.Context, podName string) {
	if a, err := c.conn(); err == nil {
		a.driver.CloseNodeShell(ctx, podName)
	}
}

var _ Driver = (*AgentCluster)(nil)

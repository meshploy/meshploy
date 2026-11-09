package k8s

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// ClusterAgentServerName is the name on every cluster agent's certificate. Each
// agent's certificate is its own and pinned, so the name proves nothing by
// itself; it is fixed so that the agent's mesh address can change.
const ClusterAgentServerName = "cluster-agent.meshploy.internal"

// IntentPrefix is where a cluster agent takes requests for the pods that reach
// the host: builds, image cleanups and node terminals.
const IntentPrefix = "/meshploy/v1/"

// PruneResult is a cluster agent's answer to an image cleanup.
type PruneResult struct {
	Removed int `json:"removed"`
}

// AgentDriver is a cluster reached through the cluster agent on its server,
// which holds the cluster's credential: the API holds none. Ordinary
// Kubernetes requests go to the agent, which checks each before sending it on
// with its own credential; the host-level pods are asked for as intents,
// which the agent carries out from its own templates.
type AgentDriver struct {
	cs  kubernetes.Interface
	cfg *rest.Config
	hc  *http.Client
}

// ErrAgentIdentity is an agent address, certificate or token that cannot be
// used.
var ErrAgentIdentity = errors.New("cluster agent identity")

// NewAgentDriver reaches the agent at addr (host:port on the mesh), trusting
// only caPEM, the agent's own certificate, and presenting token.
func NewAgentDriver(addr string, caPEM []byte, token string) (*AgentDriver, error) {
	if addr == "" || token == "" {
		return nil, fmt.Errorf("%w: an address and a token are needed", ErrAgentIdentity)
	}
	block, _ := pem.Decode(caPEM)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, fmt.Errorf("%w: the certificate is not PEM", ErrAgentIdentity)
	}
	if _, err := x509.ParseCertificate(block.Bytes); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrAgentIdentity, err)
	}
	cfg := &rest.Config{
		Host:            "https://" + addr,
		BearerToken:     token,
		TLSClientConfig: rest.TLSClientConfig{CAData: caPEM, ServerName: ClusterAgentServerName},
		Timeout:         0, // watches and streams last as long as they need
	}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	hc, err := rest.HTTPClientFor(cfg)
	if err != nil {
		return nil, err
	}
	return &AgentDriver{cs: cs, cfg: cfg, hc: hc}, nil
}

func (d *AgentDriver) Client() kubernetes.Interface { return d.cs }
func (d *AgentDriver) RestConfig() *rest.Config     { return d.cfg }

func (d *AgentDriver) StartBuild(ctx context.Context, p BuildJobParams) error {
	return d.call(ctx, http.MethodPost, "builds", p, nil)
}

func (d *AgentDriver) PruneNodeImages(ctx context.Context, node string, timeout time.Duration) (int, error) {
	// The agent bounds the cleanup itself; this leaves room for its answer.
	ctx, cancel := context.WithTimeout(ctx, timeout+30*time.Second)
	defer cancel()
	var out PruneResult
	err := d.call(ctx, http.MethodPost, "nodes/"+url.PathEscape(node)+"/prune", nil, &out)
	return out.Removed, err
}

func (d *AgentDriver) OpenNodeShell(ctx context.Context, node, podName string) (*corev1.Pod, error) {
	var pod corev1.Pod
	if err := d.call(ctx, http.MethodPost, "nodes/"+url.PathEscape(node)+"/shells/"+url.PathEscape(podName), nil, &pod); err != nil {
		return nil, err
	}
	return &pod, nil
}

func (d *AgentDriver) CloseNodeShell(ctx context.Context, podName string) {
	_ = d.call(ctx, http.MethodDelete, "shells/"+url.PathEscape(podName), nil, nil)
}

// RemoveNode asks the agent to take a node out of the cluster: the agent
// removes a worker, never the server it runs on.
func (d *AgentDriver) RemoveNode(ctx context.Context, node string) error {
	return d.call(ctx, http.MethodDelete, "nodes/"+url.PathEscape(node), nil, nil)
}

// JoinTokenRequest asks the cluster agent for a token a machine joins the
// cluster with.
type JoinTokenRequest struct {
	// Node is the joining node's name, which the token's description names.
	Node string `json:"node"`
}

// JoinToken is a k3s bootstrap token the agent minted, and when it expires.
type JoinToken struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

// JoinToken asks the agent for a token node joins the cluster with. It lasts
// as long as the agent decides, an hour.
func (d *AgentDriver) JoinToken(ctx context.Context, node string) (*JoinToken, error) {
	var out JoinToken
	if err := d.call(ctx, http.MethodPost, "k3s/join-tokens", JoinTokenRequest{Node: node}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// AgentRefusal is a cluster agent declining a request, with its reason.
type AgentRefusal struct {
	Status  int
	Message string
}

func (e *AgentRefusal) Error() string { return e.Message }

func (d *AgentDriver) call(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, d.cfg.Host+IntentPrefix+path, body)
	if err != nil {
		return err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := d.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		return &AgentRefusal{Status: resp.StatusCode, Message: strings.TrimSpace(string(raw))}
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}

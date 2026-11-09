package agent

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"testing"
	"time"

	appk8s "github.com/meshploy/packages/server/k8s"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
)

// servedAgent runs the agent behind its gate and TLS on a loopback port, as
// it runs on the mesh, and returns its address and identity.
func servedAgent(t *testing.T, a *Agent) (string, Identity) {
	t.Helper()
	id := Identity{Dir: t.TempDir()}
	if _, err := id.Ensure(); err != nil {
		t.Fatal(err)
	}
	cert, err := id.TLSCertificate()
	if err != nil {
		t.Fatal(err)
	}
	token, _ := id.Token()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := Server(ln.Addr().String(), cert, Gate{From: []string{"127.0.0.1"}, Token: token, Next: a, Logf: t.Logf})
	go srv.ServeTLS(ln, "", "")
	t.Cleanup(func() { srv.Close() })
	return ln.Addr().String(), id
}

func agentDriver(t *testing.T, addr string, id Identity) *appk8s.AgentDriver {
	t.Helper()
	certPEM, _ := id.CertPEM()
	token, _ := id.Token()
	d, err := appk8s.NewAgentDriver(addr, certPEM, token)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// The API's AgentDriver and the agent, end to end over TLS: intents carried
// out by the agent, raw host-level writes refused with Forbidden, and the
// driver's typed client working through the agent.
func TestAgentDriverThroughTheAgent(t *testing.T) {
	api := newAPIServer(t)
	a, cs, facts := testAgent(t, api)
	appk8s.RegisterJobMutator(RootlessBuilds{})
	addr, id := servedAgent(t, a)
	d := agentDriver(t, addr, id)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	if err := d.StartBuild(ctx, appk8s.BuildJobParams{JobName: "build-web-2", Namespace: "shop", Image: "ghcr.io/someone/evil", GitBranch: "main"}); err != nil {
		t.Fatalf("StartBuild: %v", err)
	}
	job, err := cs.BatchV1().Jobs("shop").Get(ctx, "build-web-2", metav1.GetOptions{})
	if err != nil || job.Spec.Template.Spec.Containers[0].Image != "ghcr.io/meshploy/builder:rootless" {
		t.Fatalf("build job: %v %v", err, job)
	}

	err = d.StartBuild(ctx, appk8s.BuildJobParams{JobName: "b", Namespace: "theirs"})
	var refusal *appk8s.AgentRefusal
	if !errors.As(err, &refusal) || refusal.Status != http.StatusForbidden {
		t.Fatalf("build in a namespace Meshploy did not make: %v", err)
	}

	if _, err := d.OpenNodeShell(ctx, "edge", "meshploy-shell-abc"); !errors.As(err, &refusal) {
		t.Fatalf("unapproved terminal: %v", err)
	}
	os.WriteFile(facts.approvalFile, []byte(time.Now().Add(time.Hour).UTC().Format(time.RFC3339)), 0o600)
	pod, err := d.OpenNodeShell(ctx, "edge", "meshploy-shell-abc")
	if err != nil || pod.Name != "meshploy-shell-abc" || pod.Spec.NodeName != "edge" {
		t.Fatalf("approved terminal: %v %+v", err, pod)
	}
	d.CloseNodeShell(ctx, "meshploy-shell-abc")
	if _, err := cs.CoreV1().Pods("kube-system").Get(ctx, "meshploy-shell-abc", metav1.GetOptions{}); !k8serrors.IsNotFound(err) {
		t.Fatalf("terminal not closed: %v", err)
	}

	if _, err := d.PruneNodeImages(ctx, "nowhere", time.Second); !errors.As(err, &refusal) {
		t.Fatalf("prune on a node not in the cluster: %v", err)
	}

	cs.CoreV1().Nodes().Create(ctx, &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "worker-1"}}, metav1.CreateOptions{})
	if err := d.RemoveNode(ctx, "worker-1"); err != nil {
		t.Fatalf("RemoveNode: %v", err)
	}
	if _, err := cs.CoreV1().Nodes().Get(ctx, "worker-1", metav1.GetOptions{}); !k8serrors.IsNotFound(err) {
		t.Fatalf("the worker is still in the cluster: %v", err)
	}

	// The typed client reaches the agent, and a raw host-level pod comes back
	// as Kubernetes' own Forbidden, which the API's code already handles.
	_, err = d.Client().CoreV1().Pods("kube-system").Create(ctx, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "x"}}, metav1.CreateOptions{})
	if !k8serrors.IsForbidden(err) {
		t.Fatalf("raw kube-system pod: %v, want Forbidden", err)
	}
	if err := appk8s.CreateBuildJob(ctx, d.Client(), appk8s.BuildJobParams{JobName: "raw", Namespace: "shop", Image: "x", GitBranch: "main"}); err == nil {
		t.Fatal("a raw privileged build job was accepted")
	}
	if len(api.realWrites()) != 0 {
		t.Fatalf("writes reached the API server: %+v", api.realWrites())
	}
}

// The driver trusts only the certificate it was given, and the agent only
// the token it made.
func TestAgentDriverIdentityIsChecked(t *testing.T) {
	api := newAPIServer(t)
	a, _, _ := testAgent(t, api)
	addr, id := servedAgent(t, a)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	other := Identity{Dir: t.TempDir()}
	other.Ensure()
	otherCert, _ := other.CertPEM()
	token, _ := id.Token()
	d, err := appk8s.NewAgentDriver(addr, otherCert, token)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Client().CoreV1().Namespaces().List(ctx, metav1.ListOptions{}); err == nil {
		t.Fatal("a driver pinning another certificate reached the agent")
	}

	certPEM, _ := id.CertPEM()
	otherToken, _ := other.Token()
	d, err = appk8s.NewAgentDriver(addr, certPEM, otherToken)
	if err != nil {
		t.Fatal(err)
	}
	_, err = d.Client().CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if !k8serrors.IsUnauthorized(err) {
		t.Fatalf("wrong token: %v, want Unauthorized", err)
	}
	if len(api.seen()) != 0 {
		t.Fatal("an unauthenticated request reached the API server")
	}

	for _, bad := range []struct{ addr, cert, token string }{
		{"", string(certPEM), token},
		{addr, "", token},
		{addr, "not pem", token},
		{addr, string(certPEM), ""},
	} {
		if _, err := appk8s.NewAgentDriver(bad.addr, []byte(bad.cert), bad.token); !errors.Is(err, appk8s.ErrAgentIdentity) {
			t.Errorf("NewAgentDriver(%q, %d bytes, %q): %v", bad.addr, len(bad.cert), bad.token, err)
		}
	}
}

// The API's cluster when its agent can register, move or be revoked while it
// runs: one client, each request to the agent of the moment, what is open to
// an old agent cut when it changes, and nothing at all without one.
func TestAgentClusterFollowsTheAgent(t *testing.T) {
	apiA, apiB := newAPIServer(t), newAPIServer(t)
	agentA, _, _ := testAgent(t, apiA)
	agentB, _, _ := testAgent(t, apiB)
	addrA, idA := servedAgent(t, agentA)
	addrB, idB := servedAgent(t, agentB)
	identity := func(addr string, id Identity) appk8s.AgentIdentity {
		cert, _ := id.CertPEM()
		token, _ := id.Token()
		return appk8s.AgentIdentity{Addr: addr, CertPEM: cert, Token: token}
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	c, err := appk8s.NewAgentCluster()
	if err != nil {
		t.Fatal(err)
	}
	cs := c.Client()

	if _, err := cs.CoreV1().Namespaces().Get(ctx, "shop", metav1.GetOptions{}); !errors.Is(err, appk8s.ErrNoClusterAgent) {
		t.Fatalf("with no agent: %v, want ErrNoClusterAgent", err)
	}
	if err := c.StartBuild(ctx, appk8s.BuildJobParams{JobName: "b", Namespace: "shop"}); !errors.Is(err, appk8s.ErrNoClusterAgent) {
		t.Fatalf("build with no agent: %v", err)
	}

	if err := c.Set(identity(addrA, idA)); err != nil {
		t.Fatal(err)
	}
	cs.CoreV1().Namespaces().Get(ctx, "shop", metav1.GetOptions{})
	if len(apiA.seen()) != 1 || len(apiB.seen()) != 0 {
		t.Fatalf("after setting A: A saw %d, B saw %d", len(apiA.seen()), len(apiB.seen()))
	}
	if got := c.RestConfig().Host; got != "https://"+addrA {
		t.Errorf("streams go to %s, want A", got)
	}

	// A watch open to A, then setting A again changes nothing.
	w, err := cs.CoreV1().Pods("shop").Watch(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("watch: %v", err)
	}
	if err := c.Set(identity(addrA, idA)); err != nil {
		t.Fatal(err)
	}
	select {
	case _, open := <-w.ResultChan():
		if !open {
			t.Fatal("setting the same agent again cut the watch")
		}
	case <-time.After(300 * time.Millisecond):
	}

	// Moving to B cuts the watch on A, and requests go to B.
	if err := c.Set(identity(addrB, idB)); err != nil {
		t.Fatal(err)
	}
	// client-go reports the cut connection as an error event, then closes.
	deadline := time.After(5 * time.Second)
drain:
	for {
		select {
		case ev, open := <-w.ResultChan():
			if !open {
				break drain
			}
			if ev.Type != watch.Error {
				t.Fatalf("the watch on A delivered a %s event instead of ending", ev.Type)
			}
		case <-deadline:
			t.Fatal("the watch on the old agent stayed open")
		}
	}
	before := len(apiA.seen())
	cs.CoreV1().Namespaces().Get(ctx, "shop", metav1.GetOptions{})
	if len(apiA.seen()) != before || len(apiB.seen()) != 1 {
		t.Fatalf("after moving to B: A saw %d more, B saw %d", len(apiA.seen())-before, len(apiB.seen()))
	}

	// B's address with A's certificate: the pinned certificate decides, so
	// nothing reaches B.
	bad := identity(addrB, idA)
	if err := c.Set(bad); err != nil {
		t.Fatal(err)
	}
	beforeB := len(apiB.seen())
	if _, err := cs.CoreV1().Namespaces().Get(ctx, "shop", metav1.GetOptions{}); err == nil {
		t.Fatal("a request reached B pinned to A's certificate")
	}
	if len(apiB.seen()) != beforeB {
		t.Fatal("B's API server saw a request under the wrong certificate")
	}

	c.Clear()
	if _, err := cs.CoreV1().Namespaces().Get(ctx, "shop", metav1.GetOptions{}); !errors.Is(err, appk8s.ErrNoClusterAgent) {
		t.Fatalf("after revoking: %v, want ErrNoClusterAgent", err)
	}
	if _, ok := c.Current(); ok {
		t.Fatal("an agent is still current after revoking")
	}
	if err := c.Set(appk8s.AgentIdentity{Addr: addrA}); !errors.Is(err, appk8s.ErrAgentIdentity) {
		t.Fatalf("an incomplete identity: %v", err)
	}
}

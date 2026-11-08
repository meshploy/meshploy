//go:build agente2e

// The test drives a real cluster through a running cluster agent, the way the
// Meshploy API does: Meshploy's own Kubernetes functions over an AgentDriver,
// then the writes the agent must refuse. Run on a machine on the agent's mesh,
// at the address the agent answers:
//
//	AGENT_ADDR=100.64.0.2:7443 AGENT_CERT=/root/cluster-agent/tls.crt AGENT_TOKEN_FILE=/root/cluster-agent/token \
//	AGENT_NODE=edge-spike AGENT_REGISTRY=10.43.159.95:5000 \
//	AGENT_SOURCE_URL=http://10.0.2.2:8765/docker.tar.gz AGENT_SOURCE_DIGEST=sha256:... \
//	./e2e.test -test.v
//
// AGENT_SHELL_APPROVED=1 says the machine's owner has approved the node
// terminal for this run; without it, opening one must be refused.

package e2e

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/meshploy/packages/hostagent"
	appk8s "github.com/meshploy/packages/server/k8s"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/client-go/transport/spdy"
)

type env struct {
	d        *appk8s.AgentDriver
	cs       kubernetes.Interface
	node     string
	registry string
}

func setup(t *testing.T) env {
	t.Helper()
	need := func(k string) string {
		v := os.Getenv(k)
		if v == "" {
			t.Skipf("%s not set", k)
		}
		return v
	}
	cert, err := os.ReadFile(need("AGENT_CERT"))
	if err != nil {
		t.Fatal(err)
	}
	token, err := os.ReadFile(need("AGENT_TOKEN_FILE"))
	if err != nil {
		t.Fatal(err)
	}
	d, err := appk8s.NewAgentDriver(need("AGENT_ADDR"), cert, strings.TrimSpace(string(token)))
	if err != nil {
		t.Fatal(err)
	}
	return env{d: d, cs: d.Client(), node: need("AGENT_NODE"), registry: os.Getenv("AGENT_REGISTRY")}
}

func ctxFor(t *testing.T, d time.Duration) context.Context {
	ctx, cancel := context.WithTimeout(t.Context(), d)
	t.Cleanup(cancel)
	return ctx
}

func step(t *testing.T, name string, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	t.Logf("ok   %s", name)
}

func refused(t *testing.T, name string, err error) {
	t.Helper()
	var r *appk8s.AgentRefusal
	if k8serrors.IsForbidden(err) || errors.As(err, &r) {
		t.Logf("ok   %s: refused", name)
		return
	}
	t.Errorf("%s: want a refusal, got %v", name, err)
}

func waitFor(ctx context.Context, what string, cond func() (bool, error)) error {
	for {
		ok, err := cond()
		if err != nil {
			return fmt.Errorf("%s: %w", what, err)
		}
		if ok {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%s: %w", what, ctx.Err())
		case <-time.After(time.Second):
		}
	}
}

// TestMeshployWorksThroughTheAgent runs what the API does to a project's
// namespace, from creating it to deleting it, through the agent.
func TestMeshployWorksThroughTheAgent(t *testing.T) {
	e := setup(t)
	ctx := ctxFor(t, 10*time.Minute)
	ns := fmt.Sprintf("agent-e2e-%04d", rand.Intn(10000))
	t.Cleanup(func() {
		if t.Failed() {
			diagnose(t, e, ns)
			if os.Getenv("AGENT_KEEP") != "" {
				t.Logf("kept namespace %s", ns)
				return
			}
		}
		appk8s.DeleteNamespace(context.Background(), e.cs, ns)
	})

	nodes, err := appk8s.GetClusterNodes(ctx, e.cs)
	step(t, "read the cluster's nodes", err)
	if len(nodes) == 0 {
		t.Fatal("no nodes")
	}
	step(t, "create the project namespace", appk8s.EnsureNamespace(ctx, e.cs, ns))
	addrs, err := appk8s.NodeTunnelAddresses(ctx, e.cs)
	step(t, "read the nodes' tunnel addresses", err)
	step(t, "isolate the namespace", appk8s.ApplyIsolation(ctx, e.cs, ns, appk8s.Isolation{NodeAddresses: addrs}))
	step(t, "isolate it again (update)", appk8s.ApplyIsolation(ctx, e.cs, ns, appk8s.Isolation{NodeAddresses: addrs, Neighbours: []string{"default"}}))
	iso, err := appk8s.IsolatedNamespaces(ctx, e.cs)
	step(t, "list isolated namespaces", err)
	if !iso[ns] {
		t.Errorf("%s not listed as isolated", ns)
	}
	step(t, "a registry pull secret", appk8s.EnsureRegistryPullSecret(ctx, e.cs, ns, "pull", "registry.example.com", "u", "p"))
	step(t, "a volume pinned to the node", appk8s.EnsureVolumePVC(ctx, e.cs, "files", ns, 1, e.node))

	web := appk8s.WorkloadParams{
		Name: "web", Namespace: ns, Image: "docker.io/library/nginx:alpine", Replicas: 1,
		Ports:        []appk8s.PortSpec{{Name: "http", Port: 80, IsPublic: true}},
		Env:          []corev1.EnvVar{{Name: "GREETING", Value: "hello"}},
		ConfigFiles:  []appk8s.ConfigFileMount{{Path: "/usr/share/nginx/html/edge.txt", Content: "served through the cluster agent\n"}},
		VolumeMounts: []appk8s.VolumeAttachment{{PVCName: "files", MountPath: "/data"}},
	}
	step(t, "its config files", appk8s.ApplyConfigSecret(ctx, e.cs, "web", ns, web.ConfigFiles))
	step(t, "deploy a service", appk8s.ApplyDeployment(ctx, e.cs, web))
	web.Env = append(web.Env, corev1.EnvVar{Name: "VERSION", Value: "2"})
	step(t, "redeploy it (update)", appk8s.ApplyDeployment(ctx, e.cs, web))
	step(t, "its cluster service", appk8s.ApplyService(ctx, e.cs, "web", ns, web.Ports))
	ports, err := appk8s.ApplyNodePortService(ctx, e.cs, "web", ns, web.Ports)
	step(t, "its published port", err)
	t.Logf("     node ports %v", ports)
	step(t, "a name service", appk8s.ApplyNameService(ctx, e.cs, "www", "web", ns, web.Ports, true))
	step(t, "prune alias services", appk8s.PruneAliasServices(ctx, e.cs, "web", ns, nil))

	avail, cancelAvail := context.WithTimeout(ctx, 3*time.Minute)
	defer cancelAvail()
	step(t, "the service becomes available", waitFor(avail, "web available", func() (bool, error) {
		st, err := appk8s.GetDeploymentState(ctx, e.cs, "web", ns)
		if st.Stalled {
			return false, errors.New("stalled")
		}
		return st.Available >= 1, err
	}))
	pods, err := appk8s.ListServicePods(ctx, e.cs, ns, "web")
	step(t, "list its pods", err)
	if len(pods) == 0 {
		t.Fatal("no pods")
	}
	pod := pods[0].Name

	var out bytes.Buffer
	step(t, "exec into the service's pod", execIn(ctx, e, ns, pod, []string{"sh", "-c", "cat /usr/share/nginx/html/edge.txt; id -u; echo $GREETING"}, &out))
	if got := out.String(); !strings.Contains(got, "served through the cluster agent") || !strings.Contains(got, "hello") {
		t.Errorf("exec printed %q", got)
	}

	logs, err := e.cs.CoreV1().Pods(ns).GetLogs(pod, &corev1.PodLogOptions{TailLines: ptr(int64(5))}).DoRaw(ctx)
	step(t, "read its logs", err)
	t.Logf("     %d bytes of log", len(logs))

	body, err := portForwardGet(ctx, e, ns, pod, 80, "/edge.txt")
	step(t, "port-forward to it", err)
	if !strings.Contains(body, "served through the cluster agent") {
		t.Errorf("port-forward GET gave %q", body)
	}

	step(t, "scale it to 2", appk8s.ScaleDeployment(ctx, e.cs, "web", ns, 2))
	step(t, "scale it to 1", appk8s.ScaleDeployment(ctx, e.cs, "web", ns, 1))
	step(t, "pin it to a node", appk8s.SetDeploymentNode(ctx, e.cs, "web", ns, e.node))

	step(t, "a database", appk8s.ApplyDatabaseDeployment(ctx, e.cs, appk8s.DatabaseWorkloadParams{
		Name: "db", Namespace: ns, Image: "docker.io/library/registry:2", Port: 5000, StorageGB: 1, DataPath: "/var/lib/registry", NodeName: e.node,
	}))

	step(t, "a one-off job", appk8s.CreateRunJob(ctx, e.cs, appk8s.RunJobParams{
		JobName: "once", Namespace: ns, Image: "docker.io/library/busybox:stable", Command: "echo ran through the cluster agent",
		CPURequest: "50m", CPULimit: "500m", MemRequest: "32Mi", MemLimit: "128Mi",
	}))
	res := appk8s.WaitForRunJob(ctx, e.cs, ns, "once", 3*time.Minute)
	if !res.Success {
		t.Fatalf("one-off job failed: %s", res.Log)
	}
	t.Logf("ok   the one-off job ran: %q", strings.TrimSpace(res.Log))

	step(t, "a cron job", appk8s.ApplyCronJob(ctx, e.cs, appk8s.CronJobParams{
		Name: "nightly", Namespace: ns, Schedule: "0 3 * * *", Image: "docker.io/library/busybox:stable", Command: "true",
		CPURequest: "50m", CPULimit: "500m", MemRequest: "32Mi", MemLimit: "128Mi",
	}))
	step(t, "update the cron job", appk8s.ApplyCronJob(ctx, e.cs, appk8s.CronJobParams{
		Name: "nightly", Namespace: ns, Schedule: "0 4 * * *", Image: "docker.io/library/busybox:stable", Command: "true",
		CPURequest: "50m", CPULimit: "500m", MemRequest: "32Mi", MemLimit: "128Mi",
	}))
	step(t, "delete the cron job", appk8s.DeleteCronJob(ctx, e.cs, ns, "nightly"))

	t.Run("sandbox", func(t *testing.T) { sandbox(t, e) })

	t.Run("node roles", func(t *testing.T) { nodeRoles(t, e) })
	t.Run("build", func(t *testing.T) { build(t, e, ns) })

	n, err := e.d.PruneNodeImages(ctx, e.node, 90*time.Second)
	step(t, "clean the node's unused images, by intent", err)
	t.Logf("     removed %d images", n)

	t.Run("node terminal", func(t *testing.T) { terminal(t, e) })

	step(t, "delete the service", appk8s.DeleteWorkload(ctx, e.cs, "web", ns))
	step(t, "delete the database's volume", appk8s.DeleteDatabasePVC(ctx, e.cs, "db", ns))
	step(t, "delete the volume", appk8s.DeleteVolumePVC(ctx, e.cs, "files", ns))
	step(t, "remove the isolation", appk8s.RemoveIsolation(ctx, e.cs, ns))
	step(t, "delete the project namespace", appk8s.DeleteNamespace(ctx, e.cs, ns))
}

// sandbox switches a namespace's workloads to gVisor and back. The
// namespace runs nothing (its deployment has no replicas), so a node without
// gVisor rejects no pods.
func sandbox(t *testing.T, e env) {
	ctx := ctxFor(t, 2*time.Minute)
	ns := fmt.Sprintf("agent-e2e-sandbox-%04d", rand.Intn(10000))
	t.Cleanup(func() { appk8s.DeleteNamespace(context.Background(), e.cs, ns) })
	step(t, "a namespace with an idle service", appk8s.EnsureNamespace(ctx, e.cs, ns))
	step(t, "the idle service", appk8s.ApplyDeployment(ctx, e.cs, appk8s.WorkloadParams{Name: "idle", Namespace: ns, Image: "docker.io/library/nginx:alpine", Replicas: 1}))
	step(t, "scaled to nothing", appk8s.ScaleDeployment(ctx, e.cs, "idle", ns, 0))
	step(t, "the sandbox runtime class", appk8s.EnsureSandboxRuntimeClass(ctx, e.cs))
	step(t, "run the namespace sandboxed", appk8s.SetNamespaceRuntime(ctx, e.cs, ns, appk8s.SandboxRuntimeClass, nil))
	d, err := e.cs.AppsV1().Deployments(ns).Get(ctx, "idle", metav1.GetOptions{})
	step(t, "read the service back", err)
	if rc := d.Spec.Template.Spec.RuntimeClassName; rc == nil || *rc != appk8s.SandboxRuntimeClass {
		t.Errorf("runtime class %v, want gvisor", rc)
	}
	step(t, "and back on the host", appk8s.SetNamespaceRuntime(ctx, e.cs, ns, "", nil))
}

func nodeRoles(t *testing.T, e env) {
	ctx := ctxFor(t, time.Minute)
	read := func() (string, bool) {
		n, err := e.cs.CoreV1().Nodes().Get(ctx, e.node, metav1.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		tainted := false
		for _, tt := range n.Spec.Taints {
			if tt.Key == appk8s.BuilderNodeLabel {
				tainted = true
			}
		}
		return n.Labels[appk8s.BuilderNodeLabel], tainted
	}
	step(t, "builds only", appk8s.SetNodeMeshRole(ctx, e.cs, e.node, appk8s.NodePlacement{InCluster: true, Builds: true, BuildsOnly: true}))
	if l, tt := read(); l != "builder" || !tt {
		t.Errorf("builds only: label %q taint %v", l, tt)
	}
	step(t, "workloads only", appk8s.SetNodeMeshRole(ctx, e.cs, e.node, appk8s.NodePlacement{InCluster: true}))
	if l, tt := read(); l != "" || tt {
		t.Errorf("workloads only: label %q taint %v", l, tt)
	}
	step(t, "builds and workloads", appk8s.SetNodeMeshRole(ctx, e.cs, e.node, appk8s.NodePlacement{InCluster: true, Builds: true}))
	if l, tt := read(); l != "builder" || tt {
		t.Errorf("builds and workloads: label %q taint %v", l, tt)
	}
}

func build(t *testing.T, e env, ns string) {
	url, digest := os.Getenv("AGENT_SOURCE_URL"), os.Getenv("AGENT_SOURCE_DIGEST")
	if url == "" || e.registry == "" {
		t.Skip("AGENT_SOURCE_URL or AGENT_REGISTRY not set")
	}
	ctx := ctxFor(t, 10*time.Minute)
	step(t, "the build cache, pinned to the build node", appk8s.EnsureBuildCachePVC(ctx, e.cs, ns, e.node))
	job := "build-web-" + fmt.Sprint(time.Now().Unix())
	dest := e.registry + "/" + ns + "/web:1"
	step(t, "start a build, by intent, naming another builder", e.d.StartBuild(ctx, appk8s.BuildJobParams{
		JobName: job, Namespace: ns, Image: "ghcr.io/someone-else/builder:evil", Builder: "dockerfile",
		Source: &appk8s.UploadedSource{URL: url, Digest: digest, Name: "docker"}, ImageDest: dest, RegistryHost: e.registry,
		BuilderNode: e.node,
	}))
	j, err := e.cs.BatchV1().Jobs(ns).Get(ctx, job, metav1.GetOptions{})
	step(t, "read the build job", err)
	c := j.Spec.Template.Spec.Containers[0]
	if strings.Contains(c.Image, "someone-else") {
		t.Fatalf("the build ran the image the API named: %s", c.Image)
	}
	if sc := c.SecurityContext; sc == nil || sc.Privileged == nil || *sc.Privileged || sc.RunAsUser == nil || *sc.RunAsUser != 1000 {
		t.Fatalf("build container security %+v", sc)
	}
	t.Logf("     build runs %s as uid 1000, unprivileged", c.Image)
	res := appk8s.WaitForJob(ctx, e.cs, ns, job, 8*time.Minute)
	if !res.Success {
		t.Fatalf("build failed: %s", res.Log)
	}
	t.Logf("ok   the build pushed %s", dest)

	step(t, "run what it built", appk8s.ApplyDeployment(ctx, e.cs, appk8s.WorkloadParams{Name: "built", Namespace: ns, Image: dest, Replicas: 1}))
	step(t, "it runs", waitFor(ctx, "built available", func() (bool, error) {
		st, err := appk8s.GetDeploymentState(ctx, e.cs, "built", ns)
		if st.Stalled {
			return false, errors.New("stalled")
		}
		return st.Available >= 1, err
	}))
	step(t, "delete it", appk8s.DeleteWorkload(ctx, e.cs, "built", ns))
}

func terminal(t *testing.T, e env) {
	ctx := ctxFor(t, 2*time.Minute)
	name := fmt.Sprintf("meshploy-shell-e2e%d", time.Now().Unix()%100000)
	pod, err := e.d.OpenNodeShell(ctx, e.node, name)
	if os.Getenv("AGENT_SHELL_APPROVED") == "" {
		refused(t, "open the node terminal without the owner's approval", err)
		return
	}
	step(t, "open the node terminal, approved", err)
	defer e.d.CloseNodeShell(context.Background(), pod.Name)
	step(t, "the terminal's pod runs", appk8s.WaitForPodRunning(ctx, e.cs, pod.Name))
	var out bytes.Buffer
	step(t, "exec into the terminal", execIn(ctx, e, "kube-system", pod.Name, []string{"chroot", "/host", "sh", "-c", "hostname; id -u"}, &out))
	t.Logf("     printed %q", out.String())
	coredns, err := e.cs.CoreV1().Pods("kube-system").List(ctx, metav1.ListOptions{LabelSelector: "k8s-app=kube-dns"})
	step(t, "find coredns", err)
	if len(coredns.Items) > 0 {
		refused(t, "exec into coredns while the terminal is approved", execIn(ctx, e, "kube-system", coredns.Items[0].Name, []string{"true"}, io.Discard))
	}
	e.d.CloseNodeShell(ctx, pod.Name)
	step(t, "the terminal closes", waitFor(ctx, "terminal gone", func() (bool, error) {
		_, err := e.cs.CoreV1().Pods("kube-system").Get(ctx, pod.Name, metav1.GetOptions{})
		return k8serrors.IsNotFound(err), nil
	}))
}

// TestTheAgentRefuses tries what the agent must never let through, against
// the real cluster, and checks each left nothing behind.
func TestTheAgentRefuses(t *testing.T) {
	e := setup(t)
	ctx := ctxFor(t, 5*time.Minute)
	ns := fmt.Sprintf("agent-e2e-attack-%04d", rand.Intn(10000))
	step(t, "a project namespace to attack from", appk8s.EnsureNamespace(ctx, e.cs, ns))
	t.Cleanup(func() { appk8s.DeleteNamespace(context.Background(), e.cs, ns) })
	step(t, "a plain deployment to patch", appk8s.ApplyDeployment(ctx, e.cs, appk8s.WorkloadParams{Name: "victim", Namespace: ns, Image: "docker.io/library/nginx:alpine", Replicas: 1}))

	yes := true
	podWith := func(name string, f func(*corev1.PodSpec)) *corev1.Pod {
		p := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns}, Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "c", Image: "docker.io/library/busybox:stable", Command: []string{"sleep", "3600"}}}}}
		f(&p.Spec)
		return p
	}
	attacks := map[string]*corev1.Pod{
		"privileged": podWith("a-priv", func(s *corev1.PodSpec) { s.Containers[0].SecurityContext = &corev1.SecurityContext{Privileged: &yes} }),
		"host path /": podWith("a-hostpath", func(s *corev1.PodSpec) {
			s.Volumes = []corev1.Volume{{Name: "h", VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/"}}}}
		}),
		"host network": podWith("a-hostnet", func(s *corev1.PodSpec) { s.HostNetwork = true }),
		"host PID":     podWith("a-hostpid", func(s *corev1.PodSpec) { s.HostPID = true }),
		"host port 443": podWith("a-hostport", func(s *corev1.PodSpec) {
			s.Containers[0].Ports = []corev1.ContainerPort{{ContainerPort: 443, HostPort: 443}}
		}),
		"SYS_ADMIN": podWith("a-cap", func(s *corev1.PodSpec) {
			s.Containers[0].SecurityContext = &corev1.SecurityContext{Capabilities: &corev1.Capabilities{Add: []corev1.Capability{"SYS_ADMIN"}}}
		}),
		"seccomp off": podWith("a-seccomp", func(s *corev1.PodSpec) {
			s.SecurityContext = &corev1.PodSecurityContext{SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeUnconfined}}
		}),
	}
	for name, p := range attacks {
		_, err := e.cs.CoreV1().Pods(ns).Create(ctx, p, metav1.CreateOptions{})
		refused(t, "a pod: "+name, err)
		if _, err := e.cs.CoreV1().Pods(ns).Get(ctx, p.Name, metav1.GetOptions{}); !k8serrors.IsNotFound(err) {
			t.Errorf("%s: the pod exists after the refusal", name)
		}
	}
	_, err := e.cs.CoreV1().Pods("kube-system").Create(ctx, podWith("a-ks", func(*corev1.PodSpec) {}), metav1.CreateOptions{})
	refused(t, "a plain pod in kube-system", err)
	_, err = e.cs.CoreV1().Pods("default").Create(ctx, podWith("a-default", func(*corev1.PodSpec) {}), metav1.CreateOptions{})
	refused(t, "a plain pod in default", err)

	// Patches that would make the existing deployment reach the host, in each
	// patch format.
	for name, p := range map[string]struct {
		pt   types.PatchType
		body string
	}{
		"strategic merge, privileged": {types.StrategicMergePatchType, `{"spec":{"template":{"spec":{"containers":[{"name":"victim","securityContext":{"privileged":true}}]}}}}`},
		"merge, host network":         {types.MergePatchType, `{"spec":{"template":{"spec":{"hostNetwork":true}}}}`},
		"JSON patch, host path":       {types.JSONPatchType, `[{"op":"add","path":"/spec/template/spec/volumes","value":[{"name":"h","hostPath":{"path":"/"}}]}]`},
		"escaped key, privileged":     {types.MergePatchType, `{"spec":{"template":{"spec":{"containers":[{"name":"victim","image":"docker.io/library/nginx:alpine","securityContext":{"privileged":true}}]}}}}`},
		"apply, privileged":           {types.ApplyPatchType, applyPrivileged(t, ctx, e, ns)},
	} {
		opts := metav1.PatchOptions{}
		if p.pt == types.ApplyPatchType {
			opts.FieldManager = "attacker"
			opts.Force = &yes
		}
		_, err := e.cs.AppsV1().Deployments(ns).Patch(ctx, "victim", p.pt, []byte(p.body), opts)
		refused(t, "patch the deployment: "+name, err)
	}
	dep, err := e.cs.AppsV1().Deployments(ns).Get(ctx, "victim", metav1.GetOptions{})
	step(t, "read the deployment back", err)
	if s := dep.Spec.Template.Spec; s.HostNetwork || len(s.Volumes) > 0 || (s.Containers[0].SecurityContext != nil && s.Containers[0].SecurityContext.Privileged != nil) {
		t.Errorf("the deployment was changed by a refused patch: %+v", s)
	}

	vpods, _ := e.cs.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{LabelSelector: "app=victim"})
	if len(vpods.Items) > 0 {
		_, err = e.cs.CoreV1().Pods(ns).UpdateEphemeralContainers(ctx, vpods.Items[0].Name, withEphemeral(vpods.Items[0]), metav1.UpdateOptions{})
		refused(t, "a privileged ephemeral container", err)
	}

	dyn, err := dynamic.NewForConfig(e.d.RestConfig())
	step(t, "a dynamic client", err)
	helm := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "helm.cattle.io/v1", "kind": "HelmChart",
		"metadata": map[string]any{"name": "a-helm", "namespace": ns},
		"spec":     map[string]any{"chart": "https://example.com/evil.tgz"},
	}}
	_, err = dyn.Resource(schema.GroupVersionResource{Group: "helm.cattle.io", Version: "v1", Resource: "helmcharts"}).Namespace(ns).Create(ctx, helm, metav1.CreateOptions{})
	refused(t, "a HelmChart, which k3s installs as cluster-admin", err)

	_, err = e.cs.RbacV1().RoleBindings(ns).Create(ctx, &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "a-admin"},
		RoleRef:    rbacv1.RoleRef{APIGroup: "rbac.authorization.k8s.io", Kind: "ClusterRole", Name: "cluster-admin"},
		Subjects:   []rbacv1.Subject{{Kind: "ServiceAccount", Name: "default", Namespace: ns}},
	}, metav1.CreateOptions{})
	refused(t, "a role binding to cluster-admin", err)
	_, err = e.cs.RbacV1().ClusterRoleBindings().Create(ctx, &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "a-admin"},
		RoleRef:    rbacv1.RoleRef{APIGroup: "rbac.authorization.k8s.io", Kind: "ClusterRole", Name: "cluster-admin"},
		Subjects:   []rbacv1.Subject{{Kind: "ServiceAccount", Name: "default", Namespace: ns}},
	}, metav1.CreateOptions{})
	refused(t, "a cluster role binding", err)

	_, err = e.cs.CoreV1().Services(ns).Create(ctx, &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "a-lb"},
		Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeLoadBalancer, Ports: []corev1.ServicePort{{Port: 443}}}}, metav1.CreateOptions{})
	refused(t, "a load balancer service (binds the node's 443)", err)
	_, err = e.cs.CoreV1().Services(ns).Create(ctx, &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "a-extip"},
		Spec: corev1.ServiceSpec{ExternalIPs: []string{"1.1.1.1"}, Ports: []corev1.ServicePort{{Port: 443}}}}, metav1.CreateOptions{})
	refused(t, "a service claiming external addresses", err)

	_, err = e.cs.CoreV1().Nodes().Patch(ctx, e.node, types.MergePatchType, []byte(`{"spec":{"unschedulable":true}}`), metav1.PatchOptions{})
	refused(t, "cordon the node", err)
	_, err = e.cs.CoreV1().Nodes().Patch(ctx, e.node, types.MergePatchType, []byte(`{"metadata":{"annotations":{"flannel.alpha.coreos.com/public-ip":"6.6.6.6"}}}`), metav1.PatchOptions{})
	refused(t, "move the node's tunnel address", err)
	_, err = e.cs.CoreV1().Nodes().Patch(ctx, e.node, types.MergePatchType, []byte(`{"metadata":{"labels":{"node-role.kubernetes.io/master":"true"}}}`), metav1.PatchOptions{})
	refused(t, "relabel the node", err)
	refused(t, "delete the node", e.cs.CoreV1().Nodes().Delete(ctx, e.node, metav1.DeleteOptions{}))
	n, err := e.cs.CoreV1().Nodes().Get(ctx, e.node, metav1.GetOptions{})
	step(t, "read the node back", err)
	if n.Spec.Unschedulable || n.Annotations["flannel.alpha.coreos.com/public-ip"] == "6.6.6.6" || n.Labels["node-role.kubernetes.io/master"] != "" {
		t.Error("the node was changed by a refused patch")
	}

	coredns, _ := e.cs.CoreV1().Pods("kube-system").List(ctx, metav1.ListOptions{LabelSelector: "k8s-app=kube-dns"})
	if len(coredns.Items) > 0 {
		refused(t, "delete coredns", e.cs.CoreV1().Pods("kube-system").Delete(ctx, coredns.Items[0].Name, metav1.DeleteOptions{}))
		refused(t, "exec into coredns", execIn(ctx, e, "kube-system", coredns.Items[0].Name, []string{"true"}, io.Discard))
	}
	_, err = e.cs.CoreV1().Secrets("kube-system").List(ctx, metav1.ListOptions{})
	refused(t, "read kube-system's secrets", err)
	_, err = e.cs.CoreV1().Secrets("").List(ctx, metav1.ListOptions{})
	refused(t, "read every namespace's secrets", err)

	_, err = e.cs.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "a-unlabelled"}}, metav1.CreateOptions{})
	refused(t, "a namespace without Meshploy's label", err)
	_, err = e.cs.CoreV1().Namespaces().Patch(ctx, "default", types.MergePatchType, []byte(`{"metadata":{"labels":{"managed-by":"meshploy"}}}`), metav1.PatchOptions{})
	refused(t, "claim the default namespace by labelling it", err)
	refused(t, "delete kube-system", e.cs.CoreV1().Namespaces().Delete(ctx, "kube-system", metav1.DeleteOptions{}))
	refused(t, "delete default", e.cs.CoreV1().Namespaces().Delete(ctx, "default", metav1.DeleteOptions{}))

	refused(t, "a raw privileged build job", appk8s.CreateBuildJob(ctx, e.cs, appk8s.BuildJobParams{JobName: "a-build", Namespace: ns, Image: "x", GitBranch: "main"}))
	_, err = appk8s.CreateShellPod(ctx, e.cs, e.node, "meshploy-shell-raw")
	refused(t, "a raw node terminal pod", err)
	_, err = appk8s.PruneNodeImages(ctx, e.cs, e.node, 10*time.Second)
	refused(t, "a raw image cleanup pod", err)
	_, err = e.d.OpenNodeShell(ctx, e.node, "coredns-evil")
	refused(t, "a node terminal named like coredns", err)
	err = e.d.StartBuild(ctx, appk8s.BuildJobParams{JobName: "a-ks-build", Namespace: "kube-system"})
	refused(t, "a build in kube-system", err)

	// Requests not from the API: a wrong token is answered Unauthorized.
	cert, _ := os.ReadFile(os.Getenv("AGENT_CERT"))
	bad, err := appk8s.NewAgentDriver(os.Getenv("AGENT_ADDR"), cert, "mcag-"+strings.Repeat("0", 64))
	step(t, "a driver with a wrong token", err)
	_, err = bad.Client().CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if !k8serrors.IsUnauthorized(err) {
		t.Errorf("a wrong token got %v, want Unauthorized", err)
	} else {
		t.Logf("ok   a wrong token: Unauthorized")
	}
}

// applyPrivileged is a server-side apply of the victim deployment as it is,
// with one container made privileged.
func applyPrivileged(t *testing.T, ctx context.Context, e env, ns string) string {
	d, err := e.cs.AppsV1().Deployments(ns).Get(ctx, "victim", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	yes := true
	c := d.Spec.Template.Spec.Containers[0]
	body := map[string]any{
		"apiVersion": "apps/v1", "kind": "Deployment",
		"metadata": map[string]any{"name": "victim"},
		"spec": map[string]any{
			"selector": d.Spec.Selector,
			"template": map[string]any{
				"metadata": map[string]any{"labels": d.Spec.Template.Labels},
				"spec": map[string]any{"containers": []any{map[string]any{
					"name": c.Name, "image": c.Image, "securityContext": map[string]any{"privileged": yes},
				}}},
			},
		},
	}
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// diagnose logs what a namespace's pods are waiting on, and its events.
func diagnose(t *testing.T, e env, ns string) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pods, err := e.cs.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Logf("diagnose: pods: %v", err)
		return
	}
	for _, p := range pods.Items {
		line := fmt.Sprintf("pod %s %s node=%q", p.Name, p.Status.Phase, p.Spec.NodeName)
		for _, c := range p.Status.Conditions {
			if c.Status != corev1.ConditionTrue {
				line += fmt.Sprintf(" %s=%s(%s)", c.Type, c.Status, c.Message)
			}
		}
		for _, c := range append(p.Status.InitContainerStatuses, p.Status.ContainerStatuses...) {
			if w := c.State.Waiting; w != nil {
				line += fmt.Sprintf(" %s waiting %s: %s", c.Name, w.Reason, w.Message)
			}
			if x := c.State.Terminated; x != nil {
				line += fmt.Sprintf(" %s ended %s (%d)", c.Name, x.Reason, x.ExitCode)
			}
		}
		t.Log(line)
	}
	pvcs, _ := e.cs.CoreV1().PersistentVolumeClaims(ns).List(ctx, metav1.ListOptions{})
	for _, c := range pvcs.Items {
		t.Logf("claim %s %s node=%q", c.Name, c.Status.Phase, c.Annotations[appk8s.SelectedNodeAnnotation])
	}
	evs, _ := e.cs.CoreV1().Events(ns).List(ctx, metav1.ListOptions{})
	for _, ev := range evs.Items {
		if ev.Type != corev1.EventTypeNormal {
			t.Logf("event %s %s: %s", ev.InvolvedObject.Name, ev.Reason, ev.Message)
		}
	}
}

func withEphemeral(p corev1.Pod) *corev1.Pod {
	yes := true
	p.Spec.EphemeralContainers = append(p.Spec.EphemeralContainers, corev1.EphemeralContainer{EphemeralContainerCommon: corev1.EphemeralContainerCommon{
		Name: "a-eph", Image: "docker.io/library/busybox:stable", SecurityContext: &corev1.SecurityContext{Privileged: &yes}}})
	return &p
}

func execIn(ctx context.Context, e env, ns, pod string, cmd []string, out io.Writer) error {
	req := e.cs.CoreV1().RESTClient().Post().Resource("pods").Namespace(ns).Name(pod).SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{Command: cmd, Stdout: true, Stderr: true}, scheme.ParameterCodec)
	ex, err := remotecommand.NewSPDYExecutor(e.d.RestConfig(), "POST", req.URL())
	if err != nil {
		return err
	}
	var errOut bytes.Buffer
	if err := ex.StreamWithContext(ctx, remotecommand.StreamOptions{Stdout: out, Stderr: &errOut}); err != nil {
		return fmt.Errorf("%w (stderr %q)", err, errOut.String())
	}
	return nil
}

func portForwardGet(ctx context.Context, e env, ns, pod string, port int, path string) (string, error) {
	rt, upgrader, err := spdy.RoundTripperFor(e.d.RestConfig())
	if err != nil {
		return "", err
	}
	u := e.cs.CoreV1().RESTClient().Post().Resource("pods").Namespace(ns).Name(pod).SubResource("portforward").URL()
	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: rt}, "POST", u)
	stop, ready := make(chan struct{}), make(chan struct{})
	defer close(stop)
	fw, err := portforward.New(dialer, []string{fmt.Sprintf("0:%d", port)}, stop, ready, io.Discard, io.Discard)
	if err != nil {
		return "", err
	}
	errc := make(chan error, 1)
	go func() { errc <- fw.ForwardPorts() }()
	select {
	case <-ready:
	case err := <-errc:
		return "", err
	case <-ctx.Done():
		return "", ctx.Err()
	}
	ports, err := fw.GetPorts()
	if err != nil {
		return "", err
	}
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d%s", ports[0].Local, path))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	return string(b), err
}

func ptr[T any](v T) *T { return &v }

// The host agent's requests, carried by the agent to a real host agent on its
// machine (`meshploy host serve`, folder AGENT_HOST_DIR), the way an API on
// another machine asks for them: its report read back, an edge whose
// platform runs elsewhere handed over and applied as a dry run, with no
// Headscale among what it would write, and a migration refused.
func TestHostRequestsThroughTheAgent(t *testing.T) {
	e := setup(t)
	if os.Getenv("AGENT_HOST_DIR") == "" {
		t.Skip("AGENT_HOST_DIR not set")
	}
	f := e.d.HostFolder()
	ctx := ctxFor(t, 3*time.Minute)

	agent, _, err := hostagent.ReadState(f)
	if err == nil && (agent == nil || time.Since(agent.HeartbeatAt) > hostagent.StaleAfter) {
		err = fmt.Errorf("no recent heartbeat: %+v", agent)
	}
	step(t, "the host agent's report, read through the agent", err)

	snap := hostagent.EdgeSnapshot{
		PublicIP: "203.0.113.10", MeshIP: "100.64.0.2", PlatformElsewhere: true, PlatformAPI: "http://100.64.0.1:4000",
		Domains: []hostagent.EdgeDomain{
			{BaseDomain: "edge.test", InternalSubdomain: "internal", DNSMode: hostagent.EdgeDNSOnDemand, Primary: true},
		},
	}
	step(t, "the edge to serve, handed over", hostagent.WriteDesiredEdgeSnapshot(f, snap))
	req := hostagent.Request{
		ID: uuidString(), Type: hostagent.RequestDomainApply, RequestedBy: "e2e",
		RequestedAt: time.Now().UTC(), Args: map[string]string{"dry_run": "true"},
	}
	step(t, "a domain.apply, queued", hostagent.QueueRequest(f, req))

	var st *hostagent.RequestStatus
	step(t, "the host agent runs it", waitFor(ctx, "the request's status", func() (bool, error) {
		st, err = hostagent.ReadRequestStatus(f, req.ID)
		return err == nil && st != nil && st.State != hostagent.RequestRunning, err
	}))
	if st.State != hostagent.RequestSucceeded {
		t.Fatalf("domain.apply %s: %s", st.State, st.Error)
	}
	raw, _, err := hostagent.ReadResult(f, hostagent.DomainApplyFile)
	step(t, "its result, read back", err)
	var result struct {
		DryRun bool   `json:"dry_run"`
		Log    string `json:"log"`
	}
	if err := json.Unmarshal(raw, &result); err != nil || !result.DryRun {
		t.Fatalf("result %s: %v", raw, err)
	}
	if !strings.Contains(result.Log, "caddy/Caddyfile") || strings.Contains(result.Log, "headscale") {
		t.Fatalf("an edge with the platform elsewhere would write:\n%s", result.Log)
	}
	t.Logf("ok   the dry run would write Caddy and CoreDNS, and no Headscale:\n%s", result.Log)

	plan := hostagent.Request{ID: uuidString(), Type: hostagent.RequestMigratePlan, RequestedBy: "e2e", RequestedAt: time.Now().UTC()}
	refused(t, "a migration", hostagent.QueueRequest(f, plan))
	_, _, err = f.Read("state/migrate/"+hostagent.PlanFile, 1<<20)
	refused(t, "reading a migration's plan", err)
}

func uuidString() string {
	b := make([]byte, 16)
	for i := range b {
		b[i] = byte(rand.Intn(256))
	}
	b[6], b[8] = b[6]&0x0f|0x40, b[8]&0x3f|0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// A join token minted by the agent on the real k3s server: the full form,
// with the server's CA hash, lasting an hour. And the install's facts, read
// through the agent.
func TestJoinTokensAndFactsThroughTheAgent(t *testing.T) {
	e := setup(t)
	ctx := ctxFor(t, time.Minute)
	j, err := e.d.JoinToken(ctx, "e2e-probe")
	step(t, "a join token", err)
	if !strings.HasPrefix(j.Token, "K10") || !strings.Contains(j.Token, "::") {
		t.Fatalf("a join token of another shape (%d characters)", len(j.Token))
	}
	if left := time.Until(j.ExpiresAt); left > time.Hour || left < 55*time.Minute {
		t.Fatalf("expires in %s", left)
	}
	t.Logf("ok   it lasts until %s", j.ExpiresAt.Format(time.RFC3339))
	_, err = e.d.JoinToken(ctx, "../server")
	refused(t, "a join token for a name that is not one", err)

	// The k3s server takes it: its secret half authenticates to the
	// cluster's API, as a joining machine's first request does, where a
	// made-up one is refused.
	if server := os.Getenv("AGENT_K3S_SERVER"); server != "" {
		_, secret, _ := strings.Cut(j.Token, "::")
		get := func(bearer string) int {
			c := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, server+"/api", nil)
			req.Header.Set("Authorization", "Bearer "+bearer)
			resp, err := c.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			return resp.StatusCode
		}
		if got := get("abcdef.0123456789abcdef"); got != http.StatusUnauthorized {
			t.Fatalf("a made-up token: %d", got)
		}
		if got := get(secret); got != http.StatusOK {
			t.Fatalf("the minted token: %d, want the k3s server to take it", got)
		}
		t.Logf("ok   the k3s server takes it, and refuses a made-up one")
	}

	if os.Getenv("AGENT_HOST_DIR") == "" {
		return
	}
	facts, err := hostagent.ReadFacts(e.d.HostFolder())
	if err == nil && facts == nil {
		err = errors.New("no facts reported")
	}
	step(t, "the install's facts, read through the agent", err)
	t.Logf("ok   public address %q, node ports bound to %q", facts.PublicIP, facts.NodePortAddresses)
}

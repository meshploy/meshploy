package agent

import (
	"bytes"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	appk8s "github.com/meshploy/packages/server/k8s"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
)

// apiServer stands in for the cluster's API server: it records what reaches
// it, and answers a dry run with whatever stored gives.
type apiServer struct {
	*httptest.Server
	mu       sync.Mutex
	requests []seen
	stored   func(r *http.Request, body []byte) (int, []byte)
	current  []byte
}

type seen struct {
	method, path, query string
	auth                string
	impersonate         bool
	body                []byte
}

func newAPIServer(t *testing.T) *apiServer {
	s := &apiServer{}
	s.stored = func(_ *http.Request, body []byte) (int, []byte) { return 200, body }
	s.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		imp := false
		for k := range r.Header {
			if strings.HasPrefix(strings.ToLower(k), "impersonate-") {
				imp = true
			}
		}
		s.mu.Lock()
		s.requests = append(s.requests, seen{r.Method, r.URL.Path, r.URL.RawQuery, r.Header.Get("Authorization"), imp, body})
		cur := s.current
		s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Query().Get("watch") == "true" || r.URL.Query().Get("watch") == "1":
			// A watch stays open, as the API server's does, until the client
			// goes away.
			w.WriteHeader(200)
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		case r.URL.Query().Get("dryRun") == "All":
			code, out := s.stored(r, body)
			w.WriteHeader(code)
			w.Write(out)
		case r.Method == http.MethodGet && cur != nil:
			w.Write(cur)
		default:
			w.Write([]byte(`{"kind":"Status","status":"Success"}`))
		}
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *apiServer) seen() []seen {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]seen(nil), s.requests...)
}

// realWrites is every request that was neither a dry run nor a read.
func (s *apiServer) realWrites() []seen {
	var out []seen
	for _, r := range s.seen() {
		if r.method != http.MethodGet && !strings.Contains(r.query, "dryRun=All") {
			out = append(out, r)
		}
	}
	return out
}

const agentCred = "the-agents-own-credential"

func testAgent(t *testing.T, api *apiServer) (*Agent, *fake.Clientset, *ClusterFacts) {
	t.Helper()
	cs := fake.NewSimpleClientset(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "shop", Labels: map[string]string{"managed-by": "meshploy"}}},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "theirs"}},
		&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "edge"}},
	)
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: api.Certificate().Raw})
	cfg := &rest.Config{Host: api.URL, BearerToken: agentCred, TLSClientConfig: rest.TLSClientConfig{CAData: certPEM}}
	facts := NewClusterFacts(cs, filepath.Join(t.TempDir(), "approval"))
	a, err := newAgent(cs, cfg, facts, BuildOptions{Image: "ghcr.io/meshploy/builder:rootless"})
	if err != nil {
		t.Fatal(err)
	}
	a.logf = t.Logf
	return a, cs, facts
}

func do(a http.Handler, method, path string, body []byte, hdr map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, bytes.NewReader(body))
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	a.ServeHTTP(w, r)
	return w
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func deploymentWith(spec corev1.PodSpec) appsv1.Deployment {
	return appsv1.Deployment{TypeMeta: metav1.TypeMeta{Kind: "Deployment", APIVersion: "apps/v1"},
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "shop"},
		Spec:       appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{Spec: spec}}}
}

var plain = corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "x"}}}

func privileged() corev1.PodSpec {
	yes := true
	return corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "x", SecurityContext: &corev1.SecurityContext{Privileged: &yes}}}}
}

// A read reaches the API server with the agent's credential, never the
// caller's, and without impersonation.
func TestReadsCarryTheAgentsCredentialOnly(t *testing.T) {
	api := newAPIServer(t)
	a, _, _ := testAgent(t, api)
	w := do(a, "GET", "/api/v1/namespaces/shop/pods", nil, map[string]string{
		"Authorization":        "Bearer the-callers-token",
		"Impersonate-User":     "system:admin",
		"Impersonate-Group":    "system:masters",
		"Impersonate-Uid":      "0",
		"Impersonate-Extra-Is": "admin",
	})
	if w.Code != 200 {
		t.Fatalf("read refused: %d %s", w.Code, w.Body)
	}
	got := api.seen()
	if len(got) != 1 {
		t.Fatalf("%d requests reached the API server, want 1", len(got))
	}
	if got[0].auth != "Bearer "+agentCred {
		t.Errorf("API server saw Authorization %q", got[0].auth)
	}
	if got[0].impersonate {
		t.Error("an impersonation header reached the API server")
	}
}

// A refused write never reaches the API server in any form.
func TestRefusedWritesNeverReachTheAPIServer(t *testing.T) {
	api := newAPIServer(t)
	a, _, _ := testAgent(t, api)
	for _, c := range []struct{ method, path string }{
		{"POST", "/api/v1/namespaces/kube-system/pods"},
		{"DELETE", "/api/v1/namespaces/kube-system/pods/coredns"},
		{"POST", "/api/v1/namespaces/theirs/pods"},
		{"POST", "/apis/rbac.authorization.k8s.io/v1/clusterrolebindings"},
		{"DELETE", "/api/v1/nodes/edge"},
		{"POST", "/apis/helm.cattle.io/v1/namespaces/shop/helmcharts"},
	} {
		w := do(a, c.method, c.path, []byte(`{}`), nil)
		if w.Code != http.StatusForbidden {
			t.Errorf("%s %s: %d, want 403", c.method, c.path, w.Code)
		}
		var st metav1.Status
		if json.Unmarshal(w.Body.Bytes(), &st) != nil || st.Reason != metav1.StatusReasonForbidden {
			t.Errorf("%s %s: refusal is not a Kubernetes Status: %s", c.method, c.path, w.Body)
		}
	}
	if n := len(api.seen()); n != 0 {
		t.Fatalf("%d refused requests reached the API server", n)
	}
}

// A write is judged by what the API server would store, not by the body: a
// harmless body the server would turn into a privileged pod (a mutating
// webhook, a default) is refused, and only the dry run reaches the server.
func TestWritesAreJudgedByTheStoredObject(t *testing.T) {
	api := newAPIServer(t)
	api.stored = func(_ *http.Request, _ []byte) (int, []byte) { return 200, mustJSON(t, deploymentWith(privileged())) }
	a, _, _ := testAgent(t, api)
	w := do(a, "POST", "/apis/apps/v1/namespaces/shop/deployments", mustJSON(t, deploymentWith(plain)), map[string]string{"Content-Type": "application/json"})
	if w.Code != http.StatusForbidden {
		t.Fatalf("got %d, want 403: %s", w.Code, w.Body)
	}
	got := api.seen()
	if len(got) != 1 || !strings.Contains(got[0].query, "dryRun=All") {
		t.Fatalf("API server saw %+v, want one dry run", got)
	}
	if len(api.realWrites()) != 0 {
		t.Fatal("the write itself reached the API server")
	}
}

// An allowed write goes through as a dry run, then for real, with the body
// and content type the caller sent.
func TestAllowedWriteIsDryRunThenSent(t *testing.T) {
	api := newAPIServer(t)
	a, _, _ := testAgent(t, api)
	body := mustJSON(t, deploymentWith(plain))
	patch := []byte(`{"spec":{"replicas":2}}`)
	api.current = mustJSON(t, deploymentWith(plain))
	for _, c := range []struct {
		method, path, ct string
		body             []byte
	}{
		{"POST", "/apis/apps/v1/namespaces/shop/deployments", "application/json", body},
		{"PATCH", "/apis/apps/v1/namespaces/shop/deployments/web", "application/strategic-merge-patch+json", patch},
	} {
		before := len(api.seen())
		api.stored = func(r *http.Request, _ []byte) (int, []byte) {
			if r.Header.Get("Content-Type") != c.ct {
				t.Errorf("dry run sent with Content-Type %q, want %q", r.Header.Get("Content-Type"), c.ct)
			}
			return 200, body
		}
		w := do(a, c.method, c.path, c.body, map[string]string{"Content-Type": c.ct})
		if w.Code != 200 {
			t.Fatalf("%s refused: %d %s", c.method, w.Code, w.Body)
		}
		got := api.seen()[before:]
		last := got[len(got)-1]
		if !strings.Contains(got[0].query, "dryRun=All") || strings.Contains(last.query, "dryRun") {
			t.Errorf("%s: requests were %+v, want a dry run first and the write last", c.method, got)
		}
		if !bytes.Equal(last.body, c.body) {
			t.Errorf("%s: the write carried %s, want the caller's body", c.method, last.body)
		}
	}
}

// When the API server refuses the dry run, its answer goes back as it is,
// and the write is never sent.
func TestAnAPIServerRefusalIsRelayed(t *testing.T) {
	api := newAPIServer(t)
	refusal := `{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"Invalid","code":422,"message":"spec.template: Required value"}`
	api.stored = func(_ *http.Request, _ []byte) (int, []byte) { return 422, []byte(refusal) }
	a, _, _ := testAgent(t, api)
	w := do(a, "POST", "/apis/apps/v1/namespaces/shop/deployments", []byte(`{}`), map[string]string{"Content-Type": "application/json"})
	if w.Code != 422 || strings.TrimSpace(w.Body.String()) != refusal {
		t.Fatalf("got %d %s, want the API server's 422", w.Code, w.Body)
	}
	if len(api.realWrites()) != 0 {
		t.Fatal("the write was sent after the API server refused its dry run")
	}
}

// A caller cannot turn the dry run off: its own dryRun parameter is replaced.
func TestTheCallerCannotSkipTheDryRun(t *testing.T) {
	api := newAPIServer(t)
	api.stored = func(_ *http.Request, _ []byte) (int, []byte) { return 200, mustJSON(t, deploymentWith(privileged())) }
	a, _, _ := testAgent(t, api)
	w := do(a, "POST", "/apis/apps/v1/namespaces/shop/deployments?dryRun=None&dryRun=", mustJSON(t, deploymentWith(privileged())), map[string]string{"Content-Type": "application/json"})
	if w.Code != http.StatusForbidden {
		t.Fatalf("got %d, want 403", w.Code)
	}
	for _, r := range api.seen() {
		if !strings.Contains(r.query, "dryRun=All") || strings.Count(r.query, "dryRun") != 1 {
			t.Errorf("a request reached the API server with query %q", r.query)
		}
	}
}

// An oversized body is refused before anything is sent.
func TestOversizedBodiesAreRefused(t *testing.T) {
	api := newAPIServer(t)
	a, _, _ := testAgent(t, api)
	w := do(a, "POST", "/api/v1/namespaces/shop/secrets", bytes.Repeat([]byte("x"), maxBody+1), nil)
	if w.Code != http.StatusForbidden || len(api.seen()) != 0 {
		t.Fatalf("got %d with %d requests sent", w.Code, len(api.seen()))
	}
}

// A node patch is checked against the node as it is now.
func TestNodePatchIsComparedWithTheCurrentNode(t *testing.T) {
	api := newAPIServer(t)
	node := corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "edge", Labels: map[string]string{"kubernetes.io/hostname": "edge"}}}
	api.current = mustJSON(t, node)
	a, _, _ := testAgent(t, api)

	labelled := node.DeepCopy()
	labelled.Labels["meshploy.com/role"] = "builder"
	api.stored = func(_ *http.Request, _ []byte) (int, []byte) { return 200, mustJSON(t, labelled) }
	if w := do(a, "PATCH", "/api/v1/nodes/edge", []byte(`{}`), map[string]string{"Content-Type": "application/merge-patch+json"}); w.Code != 200 {
		t.Fatalf("role label refused: %d %s", w.Code, w.Body)
	}
	cordoned := node.DeepCopy()
	cordoned.Spec.Unschedulable = true
	api.stored = func(_ *http.Request, _ []byte) (int, []byte) { return 200, mustJSON(t, cordoned) }
	if w := do(a, "PATCH", "/api/v1/nodes/edge", []byte(`{}`), map[string]string{"Content-Type": "application/merge-patch+json"}); w.Code != http.StatusForbidden {
		t.Fatalf("cordoning allowed: %d", w.Code)
	}
}

func TestIntentBuildUsesTheAgentsBuilder(t *testing.T) {
	api := newAPIServer(t)
	a, cs, _ := testAgent(t, api)
	appk8s.RegisterJobMutator(RootlessBuilds{})
	p := appk8s.BuildJobParams{JobName: "build-web-1", Namespace: "shop", Image: "ghcr.io/someone/evil:latest", GitBranch: "main"}
	w := do(a, "POST", IntentPrefix+"builds", mustJSON(t, p), nil)
	if w.Code != 200 {
		t.Fatalf("build refused: %d %s", w.Code, w.Body)
	}
	job, err := cs.BatchV1().Jobs("shop").Get(t.Context(), "build-web-1", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	c := job.Spec.Template.Spec.Containers[0]
	if c.Image != "ghcr.io/meshploy/builder:rootless" {
		t.Errorf("build ran %s, want the agent's builder", c.Image)
	}
	if sc := c.SecurityContext; sc == nil || sc.Privileged == nil || *sc.Privileged || sc.RunAsUser == nil || *sc.RunAsUser != 1000 {
		t.Errorf("build container security %+v, want unprivileged uid 1000", sc)
	}
	if sc := c.SecurityContext; sc.SELinuxOptions == nil || sc.SELinuxOptions.Type != BuilderSELinuxType {
		t.Errorf("build container SELinux %+v, want %s", sc.SELinuxOptions, BuilderSELinuxType)
	}
	// Only BuildKit's cache, in a folder no root builder wrote.
	var cache []corev1.VolumeMount
	for _, m := range c.VolumeMounts {
		if m.Name == appk8s.BuildCacheVolume {
			cache = append(cache, m)
		}
	}
	if len(cache) != 1 || cache[0].SubPath != RootlessCacheSubPath || cache[0].MountPath != "/tmp/buildkit-root" {
		t.Errorf("build cache mounts %+v, want BuildKit's alone, in %s", cache, RootlessCacheSubPath)
	}
	scratch := false
	for _, m := range c.VolumeMounts {
		scratch = scratch || m.Name == appk8s.BuildScratchVolume
	}
	if !scratch {
		t.Error("a rootless build lost its scratch folder on the node")
	}
	if ps := job.Spec.Template.Spec.SecurityContext; ps.FSGroupChangePolicy == nil || *ps.FSGroupChangePolicy != corev1.FSGroupChangeOnRootMismatch {
		t.Error("the build cache is handed over on every build, not once")
	}
	if len(api.seen()) != 0 {
		t.Error("an intent was forwarded to the API server as a request")
	}
}

func TestIntentBuildRefusals(t *testing.T) {
	api := newAPIServer(t)
	a, cs, _ := testAgent(t, api)
	cases := map[string][]byte{
		"unmanaged namespace": mustJSON(t, appk8s.BuildJobParams{JobName: "b", Namespace: "theirs"}),
		"kube-system":         mustJSON(t, appk8s.BuildJobParams{JobName: "b", Namespace: "kube-system"}),
		"no name":             mustJSON(t, appk8s.BuildJobParams{Namespace: "shop"}),
		"unknown field":       []byte(`{"JobName":"b","Namespace":"shop","Privileged":true}`),
		"not JSON":            []byte(`{`),
	}
	for name, body := range cases {
		if w := do(a, "POST", IntentPrefix+"builds", body, nil); w.Code != http.StatusForbidden {
			t.Errorf("%s: %d, want 403", name, w.Code)
		}
	}
	a.builds.Image = ""
	if w := do(a, "POST", IntentPrefix+"builds", mustJSON(t, appk8s.BuildJobParams{JobName: "b", Namespace: "shop"}), nil); w.Code != http.StatusForbidden {
		t.Errorf("a build with no builder set up: %d, want 403", w.Code)
	}
	jobs, _ := cs.BatchV1().Jobs("").List(t.Context(), metav1.ListOptions{})
	if len(jobs.Items) != 0 {
		t.Errorf("%d jobs made by refused builds", len(jobs.Items))
	}
}

func TestIntentShell(t *testing.T) {
	api := newAPIServer(t)
	a, cs, facts := testAgent(t, api)
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	a.timeNow = func() time.Time { return now }

	open := func(node, name string) int {
		return do(a, "POST", IntentPrefix+"nodes/"+node+"/shells/"+name, nil, nil).Code
	}
	if c := open("edge", "meshploy-shell-1"); c != http.StatusForbidden {
		t.Fatalf("unapproved terminal: %d, want 403", c)
	}
	if err := os.WriteFile(facts.approvalFile, []byte(now.Add(-time.Minute).Format(time.RFC3339)), 0o600); err != nil {
		t.Fatal(err)
	}
	if c := open("edge", "meshploy-shell-1"); c != http.StatusForbidden {
		t.Fatalf("expired approval: %d, want 403", c)
	}
	os.WriteFile(facts.approvalFile, []byte("garbage"), 0o600)
	if c := open("edge", "meshploy-shell-1"); c != http.StatusForbidden {
		t.Fatalf("unreadable approval: %d, want 403", c)
	}
	os.WriteFile(facts.approvalFile, []byte(now.Add(time.Hour).Format(time.RFC3339)+"\n"), 0o600)
	for _, bad := range []string{"coredns-abc", "meshploy-shell-", "meshploy-shell-ABC", "meshploy-shell-1.x"} {
		if c := open("edge", bad); c != http.StatusForbidden {
			t.Errorf("terminal named %q: %d, want 403", bad, c)
		}
	}
	if c := open("nowhere", "meshploy-shell-1"); c != http.StatusForbidden {
		t.Errorf("terminal on a node not in the cluster: %d, want 403", c)
	}
	if c := open("edge", "meshploy-shell-1"); c != 200 {
		t.Fatalf("approved terminal: %d", c)
	}
	pod, err := cs.CoreV1().Pods("kube-system").Get(t.Context(), "meshploy-shell-1", metav1.GetOptions{})
	if err != nil || pod.Spec.NodeName != "edge" {
		t.Fatalf("terminal pod: %v %+v", err, pod)
	}

	// Closing: a pod that is not a terminal is refused, a gone one is fine.
	cs.CoreV1().Pods("kube-system").Create(t.Context(), &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "meshploy-shell-fake", Namespace: "kube-system"}}, metav1.CreateOptions{})
	if c := do(a, "DELETE", IntentPrefix+"shells/meshploy-shell-fake", nil, nil).Code; c != http.StatusForbidden {
		t.Errorf("closing an unlabelled pod: %d, want 403", c)
	}
	if c := do(a, "DELETE", IntentPrefix+"shells/coredns-abc", nil, nil).Code; c != http.StatusForbidden {
		t.Errorf("closing coredns: %d, want 403", c)
	}
	if c := do(a, "DELETE", IntentPrefix+"shells/meshploy-shell-1", nil, nil).Code; c != 200 {
		t.Errorf("closing the terminal: %d", c)
	}
	if _, err := cs.CoreV1().Pods("kube-system").Get(t.Context(), "meshploy-shell-1", metav1.GetOptions{}); err == nil {
		t.Error("terminal still there")
	}
	if c := do(a, "DELETE", IntentPrefix+"shells/meshploy-shell-1", nil, nil).Code; c != 200 {
		t.Errorf("closing a closed terminal: %d", c)
	}
	if _, err := cs.CoreV1().Pods("kube-system").Get(t.Context(), "meshploy-shell-fake", metav1.GetOptions{}); err != nil {
		t.Error("the unlabelled pod was deleted")
	}
}

func TestUnknownIntentsAreRefused(t *testing.T) {
	api := newAPIServer(t)
	a, _, _ := testAgent(t, api)
	for _, c := range []struct{ method, path string }{
		{"GET", IntentPrefix + "builds"},
		{"POST", IntentPrefix + "nodes/edge/delete"},
		{"POST", IntentPrefix + "exec"},
		{"DELETE", IntentPrefix + "nodes/edge/shells"},
	} {
		if w := do(a, c.method, c.path, nil, nil); w.Code != http.StatusNotFound {
			t.Errorf("%s %s: %d, want 404", c.method, c.path, w.Code)
		}
	}
	if len(api.seen()) != 0 {
		t.Error("an unknown intent reached the API server")
	}
}

func TestRootlessBuildsLeavesOtherJobsAlone(t *testing.T) {
	job := &batchv1.Job{Spec: batchv1.JobSpec{Template: corev1.PodTemplateSpec{Spec: plain}}}
	if err := (RootlessBuilds{}).MutateJob(t.Context(), appk8s.JobRun, job); err != nil {
		t.Fatal(err)
	}
	if job.Spec.Template.Spec.Containers[0].SecurityContext != nil || job.Spec.Template.Spec.SecurityContext != nil {
		t.Error("a run job was changed")
	}
}

// With a client certificate as its credential, as k3s's own kubeconfig has,
// nothing replaces the caller's Authorization header: it must be dropped, or
// the API's token would reach the cluster.
func TestTheCallersTokenNeverReachesTheCluster(t *testing.T) {
	api := newAPIServer(t)
	a, _, _ := testAgent(t, api)
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: api.Certificate().Raw})
	a2, err := newAgent(a.cs, &rest.Config{Host: api.URL, TLSClientConfig: rest.TLSClientConfig{CAData: certPEM}}, a.facts, a.builds)
	if err != nil {
		t.Fatal(err)
	}
	a2.logf = t.Logf
	if w := do(a2, "GET", "/api/v1/namespaces/shop/pods", nil, map[string]string{"Authorization": "Bearer mcag-the-apis-token"}); w.Code != 200 {
		t.Fatalf("read refused: %d", w.Code)
	}
	if got := api.seen(); len(got) != 1 || got[0].auth != "" {
		t.Fatalf("API server saw Authorization %q", got[0].auth)
	}
}

// A machine leaving takes its node out of the cluster through the agent: a
// worker is removed, one already gone is no error, and the server never is.
func TestTheAgentRemovesAWorkerButNeverTheServer(t *testing.T) {
	api := newAPIServer(t)
	a, cs, _ := testAgent(t, api)
	ctx := t.Context()
	edge, _ := cs.CoreV1().Nodes().Get(ctx, "edge", metav1.GetOptions{})
	edge.Labels = map[string]string{"node-role.kubernetes.io/control-plane": "true"}
	cs.CoreV1().Nodes().Update(ctx, edge, metav1.UpdateOptions{})
	cs.CoreV1().Nodes().Create(ctx, &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "worker-1"}}, metav1.CreateOptions{})

	if c := do(a, "DELETE", IntentPrefix+"nodes/edge", nil, nil).Code; c != http.StatusForbidden {
		t.Errorf("removing the server: %d, want 403", c)
	}
	if _, err := cs.CoreV1().Nodes().Get(ctx, "edge", metav1.GetOptions{}); err != nil {
		t.Fatalf("the server was removed: %v", err)
	}
	if c := do(a, "DELETE", IntentPrefix+"nodes/worker-1", nil, nil).Code; c != 200 {
		t.Errorf("removing a worker: %d", c)
	}
	if _, err := cs.CoreV1().Nodes().Get(ctx, "worker-1", metav1.GetOptions{}); !k8serrors.IsNotFound(err) {
		t.Errorf("the worker is still there: %v", err)
	}
	if c := do(a, "DELETE", IntentPrefix+"nodes/worker-1", nil, nil).Code; c != 200 {
		t.Errorf("removing a worker already gone: %d", c)
	}
}

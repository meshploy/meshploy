package policy

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	nodev1 "k8s.io/api/node/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var now = time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)

type facts struct {
	managed  map[string]bool
	approved time.Time
	shells   map[string]bool
}

func (f facts) Managed(ns string) bool           { return f.managed[ns] }
func (f facts) TerminalApprovedUntil() time.Time { return f.approved }
func (f facts) Shell(name string) bool           { return f.shells[name] }

var cluster = facts{
	managed: map[string]bool{"shop": true, "shop-staging": true},
	shells:  map[string]bool{"meshploy-shell-1": true},
}

func approved() facts {
	f := cluster
	f.approved = now.Add(time.Hour)
	return f
}

func js(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// app is an ordinary workload pod, as Meshploy deploys one.
func app() corev1.PodSpec {
	return corev1.PodSpec{
		Containers: []corev1.Container{{Name: "app", Image: "registry:5000/shop/web:1"}},
		Volumes: []corev1.Volume{
			{Name: "data", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "data"}}},
			{Name: "cfg", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: "cfg"}}},
			{Name: "tmp", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
		},
	}
}

func pod(spec corev1.PodSpec) corev1.Pod {
	return corev1.Pod{TypeMeta: metav1.TypeMeta{Kind: "Pod", APIVersion: "v1"}, ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "shop"}, Spec: spec}
}

func deployment(spec corev1.PodSpec) appsv1.Deployment {
	return appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "shop"},
		Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{Spec: spec}}}
}

func job(spec corev1.PodSpec) batchv1.Job {
	return batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "j", Namespace: "shop"},
		Spec: batchv1.JobSpec{Template: corev1.PodTemplateSpec{Spec: spec}}}
}

func cronjob(spec corev1.PodSpec) batchv1.CronJob {
	return batchv1.CronJob{ObjectMeta: metav1.ObjectMeta{Name: "c", Namespace: "shop"},
		Spec: batchv1.CronJobSpec{Schedule: "* * * * *", JobTemplate: batchv1.JobTemplateSpec{Spec: batchv1.JobSpec{Template: corev1.PodTemplateSpec{Spec: spec}}}}}
}

func with(f func(*corev1.PodSpec)) corev1.PodSpec {
	s := app()
	f(&s)
	return s
}

func yes() *bool { b := true; return &b }

// The pod specs that reach past their container. Every one must be refused
// in every kind that carries a pod.
func attacks() map[string]corev1.PodSpec {
	unmasked := corev1.UnmaskedProcMount
	return map[string]corev1.PodSpec{
		"privileged": with(func(s *corev1.PodSpec) { s.Containers[0].SecurityContext = &corev1.SecurityContext{Privileged: yes()} }),
		"privileged init": with(func(s *corev1.PodSpec) {
			s.InitContainers = []corev1.Container{{Name: "i", Image: "x", SecurityContext: &corev1.SecurityContext{Privileged: yes()}}}
		}),
		"privileged ephemeral": with(func(s *corev1.PodSpec) {
			s.EphemeralContainers = []corev1.EphemeralContainer{{EphemeralContainerCommon: corev1.EphemeralContainerCommon{Name: "e", Image: "x", SecurityContext: &corev1.SecurityContext{Privileged: yes()}}}}
		}),
		"host network": with(func(s *corev1.PodSpec) { s.HostNetwork = true }),
		"host PID":     with(func(s *corev1.PodSpec) { s.HostPID = true }),
		"host IPC":     with(func(s *corev1.PodSpec) { s.HostIPC = true }),
		"host path": with(func(s *corev1.PodSpec) {
			s.Volumes = append(s.Volumes, corev1.Volume{Name: "root", VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/"}}})
		}),
		"host port": with(func(s *corev1.PodSpec) {
			s.Containers[0].Ports = []corev1.ContainerPort{{ContainerPort: 443, HostPort: 443}}
		}),
		"SYS_ADMIN": with(func(s *corev1.PodSpec) {
			s.Containers[0].SecurityContext = &corev1.SecurityContext{Capabilities: &corev1.Capabilities{Add: []corev1.Capability{"SYS_ADMIN"}}}
		}),
		"NET_RAW (baseline allows)": with(func(s *corev1.PodSpec) {
			s.Containers[0].SecurityContext = &corev1.SecurityContext{Capabilities: &corev1.Capabilities{Add: []corev1.Capability{"NET_RAW"}}}
		}),
		"seccomp unconfined pod": with(func(s *corev1.PodSpec) {
			s.SecurityContext = &corev1.PodSecurityContext{SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeUnconfined}}
		}),
		"seccomp unconfined": with(func(s *corev1.PodSpec) {
			s.Containers[0].SecurityContext = &corev1.SecurityContext{SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeUnconfined}}
		}),
		"seccomp from the node": with(func(s *corev1.PodSpec) {
			lp := "x.json"
			s.Containers[0].SecurityContext = &corev1.SecurityContext{SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeLocalhost, LocalhostProfile: &lp}}
		}),
		"AppArmor unconfined": with(func(s *corev1.PodSpec) {
			s.Containers[0].SecurityContext = &corev1.SecurityContext{AppArmorProfile: &corev1.AppArmorProfile{Type: corev1.AppArmorProfileTypeUnconfined}}
		}),
		"AppArmor from the node pod": with(func(s *corev1.PodSpec) {
			lp := "x"
			s.SecurityContext = &corev1.PodSecurityContext{AppArmorProfile: &corev1.AppArmorProfile{Type: corev1.AppArmorProfileTypeLocalhost, LocalhostProfile: &lp}}
		}),
		"unmasked /proc": with(func(s *corev1.PodSpec) {
			s.Containers[0].SecurityContext = &corev1.SecurityContext{ProcMount: &unmasked}
		}),
		"unsafe sysctl": with(func(s *corev1.PodSpec) {
			s.SecurityContext = &corev1.PodSecurityContext{Sysctls: []corev1.Sysctl{{Name: "kernel.core_pattern", Value: "|/tmp/x"}}}
		}),
		"SELinux type": with(func(s *corev1.PodSpec) {
			s.Containers[0].SecurityContext = &corev1.SecurityContext{SELinuxOptions: &corev1.SELinuxOptions{Type: "spc_t"}}
		}),
		"Windows host process": with(func(s *corev1.PodSpec) {
			s.SecurityContext = &corev1.PodSecurityContext{WindowsOptions: &corev1.WindowsSecurityContextOptions{HostProcess: yes()}}
		}),
		"NFS volume": with(func(s *corev1.PodSpec) {
			s.Volumes = append(s.Volumes, corev1.Volume{Name: "n", VolumeSource: corev1.VolumeSource{NFS: &corev1.NFSVolumeSource{Server: "10.0.0.1", Path: "/"}}})
		}),
		"CSI inline volume": with(func(s *corev1.PodSpec) {
			s.Volumes = append(s.Volumes, corev1.Volume{Name: "c", VolumeSource: corev1.VolumeSource{CSI: &corev1.CSIVolumeSource{Driver: "x"}}})
		}),
		"image volume": with(func(s *corev1.PodSpec) {
			s.Volumes = append(s.Volumes, corev1.Volume{Name: "i", VolumeSource: corev1.VolumeSource{Image: &corev1.ImageVolumeSource{Reference: "x"}}})
		}),
		"volume of no known kind": with(func(s *corev1.PodSpec) { s.Volumes = append(s.Volumes, corev1.Volume{Name: "empty"}) }),
	}
}

func decide(t *testing.T, f facts, method, path string, stored, current []byte) Decision {
	t.Helper()
	r := Request{Method: method, Path: path, Now: now}
	d := Decide(r, f)
	if d.NeedObject {
		if stored == nil {
			t.Fatalf("%s %s asked for its stored object, and the test gave none", method, path)
		}
		r.Stored, r.Current = stored, current
		d = Decide(r, f)
		if d.NeedObject {
			t.Fatalf("%s %s asked for its stored object twice", method, path)
		}
	}
	return d
}

func TestEveryAttackIsRefusedInEveryWorkloadKind(t *testing.T) {
	for name, spec := range attacks() {
		kinds := map[string]struct {
			path string
			obj  any
		}{
			"pod":        {"/api/v1/namespaces/shop/pods", pod(spec)},
			"deployment": {"/apis/apps/v1/namespaces/shop/deployments", deployment(spec)},
			"job":        {"/apis/batch/v1/namespaces/shop/jobs", job(spec)},
			"cronjob":    {"/apis/batch/v1/namespaces/shop/cronjobs", cronjob(spec)},
		}
		for kindName, k := range kinds {
			for _, method := range []string{"POST", "PUT", "PATCH"} {
				path := k.path
				if method != "POST" {
					path += "/x"
				}
				d := decide(t, cluster, method, path, js(t, k.obj), js(t, k.obj))
				if d.Allow {
					t.Errorf("%s %s (%s) was allowed: %s", method, kindName, name, d.Reason)
				}
			}
		}
	}
}

func TestMeshployWorkloadsAreAllowed(t *testing.T) {
	gvisor := "gvisor"
	runAsRoot := with(func(s *corev1.PodSpec) {
		s.RuntimeClassName = &gvisor
		s.SecurityContext = &corev1.PodSecurityContext{FSGroup: new(int64)}
		s.Containers[0].Ports = []corev1.ContainerPort{{ContainerPort: 8080}}
		s.Containers[0].Env = []corev1.EnvVar{{Name: "PORT", Value: "8080"}}
	})
	cases := map[string]struct {
		path string
		obj  any
	}{
		"pod":        {"/api/v1/namespaces/shop/pods", pod(app())},
		"deployment": {"/apis/apps/v1/namespaces/shop/deployments", deployment(app())},
		"sandboxed":  {"/apis/apps/v1/namespaces/shop/deployments", deployment(runAsRoot)},
		"job":        {"/apis/batch/v1/namespaces/shop/jobs", job(app())},
		"cronjob":    {"/apis/batch/v1/namespaces/shop-staging/cronjobs", cronjob(app())},
	}
	for name, c := range cases {
		for _, method := range []string{"POST", "PUT", "PATCH"} {
			path := c.path
			if method != "POST" {
				path += "/x"
			}
			if d := decide(t, cluster, method, path, js(t, c.obj), js(t, c.obj)); !d.Allow {
				t.Errorf("%s %s refused: %s", method, name, d.Reason)
			}
		}
	}
}

func TestWritesOutsideMeshployNamespacesAreRefused(t *testing.T) {
	for _, ns := range []string{"default", "kube-public", "someone-else"} {
		d := decide(t, cluster, "POST", "/apis/apps/v1/namespaces/"+ns+"/deployments", js(t, deployment(app())), nil)
		if d.Allow {
			t.Errorf("deployment in %s allowed", ns)
		}
	}
}

// Kinds Meshploy never writes are refused even in its own namespaces: each
// can make pods or grant rights no check here sees.
func TestKindsMeshployDoesNotMakeAreRefused(t *testing.T) {
	for _, path := range []string{
		"/apis/helm.cattle.io/v1/namespaces/shop/helmcharts",
		"/apis/helm.cattle.io/v1/namespaces/shop/helmchartconfigs",
		"/api/v1/namespaces/shop/replicationcontrollers",
		"/api/v1/namespaces/shop/podtemplates",
		"/apis/apps/v1/namespaces/shop/statefulsets",
		"/apis/apps/v1/namespaces/shop/daemonsets",
		"/apis/apps/v1/namespaces/shop/replicasets",
		"/apis/rbac.authorization.k8s.io/v1/namespaces/shop/rolebindings",
		"/apis/rbac.authorization.k8s.io/v1/namespaces/shop/roles",
		"/api/v1/namespaces/shop/serviceaccounts",
		"/api/v1/namespaces/shop/endpoints",
		"/apis/discovery.k8s.io/v1/namespaces/shop/endpointslices",
		"/api/v1/namespaces/shop/configmaps",
		"/apis/networking.k8s.io/v1/namespaces/shop/ingresses",
		"/apis/example.com/v1/namespaces/shop/widgets",
		"/api/v1/namespaces/shop/events",
	} {
		for _, method := range []string{"POST", "PUT", "PATCH", "DELETE"} {
			if d := decide(t, cluster, method, path+"/x", []byte(`{}`), []byte(`{}`)); d.Allow {
				t.Errorf("%s %s allowed: %s", method, path, d.Reason)
			}
		}
		if d := decide(t, cluster, "POST", path, []byte(`{}`), nil); d.Allow {
			t.Errorf("POST %s allowed: %s", path, d.Reason)
		}
	}
}

func TestSubresources(t *testing.T) {
	cases := []struct {
		method, path string
		allow        bool
	}{
		{"PUT", "/apis/apps/v1/namespaces/shop/deployments/web/scale", true},
		{"PATCH", "/apis/apps/v1/namespaces/shop/deployments/web/scale", true},
		{"PUT", "/apis/apps/v1/namespaces/shop/deployments/web/status", false},
		{"PATCH", "/apis/batch/v1/namespaces/shop/jobs/j/status", false},
		{"PATCH", "/api/v1/namespaces/shop/pods/p/ephemeralcontainers", false},
		{"PUT", "/api/v1/namespaces/shop/pods/p/ephemeralcontainers", false},
		{"POST", "/api/v1/namespaces/shop/pods/p/binding", false},
		{"POST", "/api/v1/namespaces/shop/pods/p/eviction", false},
		{"PUT", "/api/v1/namespaces/shop/pods/p/resize", false},
		{"POST", "/api/v1/namespaces/shop/serviceaccounts/default/token", false},
		{"POST", "/api/v1/namespaces/shop/pods/p/exec", true},
		{"GET", "/api/v1/namespaces/shop/pods/p/exec", true},
		{"POST", "/api/v1/namespaces/shop/pods/p/portforward", true},
		{"GET", "/api/v1/namespaces/shop/pods/p/log", true},
		{"POST", "/api/v1/namespaces/default/pods/p/exec", false},
		{"POST", "/apis/batch/v1/namespaces/shop/jobs/j/scale", false},
	}
	for _, c := range cases {
		d := Decide(Request{Method: c.method, Path: c.path, Now: now}, cluster)
		if d.NeedObject {
			t.Errorf("%s %s asked for its object", c.method, c.path)
			continue
		}
		if d.Allow != c.allow {
			t.Errorf("%s %s: allow=%v (%s), want %v", c.method, c.path, d.Allow, d.Reason, c.allow)
		}
	}
	// An upgrade that is not exec, attach or port-forward is refused.
	if d := Decide(Request{Method: "GET", Path: "/api/v1/namespaces/shop/pods/p/log", Upgrade: true, Now: now}, cluster); d.Allow {
		t.Error("an upgrade on the log subresource was allowed")
	}
}

func TestOtherMeshployKinds(t *testing.T) {
	node := corev1.Service{Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeNodePort, Ports: []corev1.ServicePort{{Port: 5432, NodePort: 30432}}}}
	cases := []struct {
		name, path string
		obj        any
		allow      bool
	}{
		{"cluster IP service", "/api/v1/namespaces/shop/services", corev1.Service{Spec: corev1.ServiceSpec{Ports: []corev1.ServicePort{{Port: 80}}}}, true},
		{"node port service", "/api/v1/namespaces/shop/services", node, true},
		{"load balancer", "/api/v1/namespaces/shop/services", corev1.Service{Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeLoadBalancer}}, false},
		{"external IPs", "/api/v1/namespaces/shop/services", corev1.Service{Spec: corev1.ServiceSpec{ExternalIPs: []string{"1.2.3.4"}}}, false},
		{"external name", "/api/v1/namespaces/shop/services", corev1.Service{Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeExternalName, ExternalName: "x.example"}}, false},
		{"secret", "/api/v1/namespaces/shop/secrets", corev1.Secret{StringData: map[string]string{"k": "v"}}, true},
		{"claim", "/api/v1/namespaces/shop/persistentvolumeclaims", corev1.PersistentVolumeClaim{}, true},
		{"claim naming a volume", "/api/v1/namespaces/shop/persistentvolumeclaims", corev1.PersistentVolumeClaim{Spec: corev1.PersistentVolumeClaimSpec{VolumeName: "pvc-other"}}, false},
		{"network policy", "/apis/networking.k8s.io/v1/namespaces/shop/networkpolicies", struct{}{}, true},
		{"quota", "/api/v1/namespaces/shop/resourcequotas", struct{}{}, true},
		{"limit range", "/api/v1/namespaces/shop/limitranges", struct{}{}, true},
	}
	for _, c := range cases {
		if d := decide(t, cluster, "POST", c.path, js(t, c.obj), nil); d.Allow != c.allow {
			t.Errorf("%s: allow=%v (%s), want %v", c.name, d.Allow, d.Reason, c.allow)
		}
	}
	// A bound claim keeps its volume name through an update.
	bound := corev1.PersistentVolumeClaim{Spec: corev1.PersistentVolumeClaimSpec{VolumeName: "pvc-1"}}
	if d := decide(t, cluster, "PUT", "/api/v1/namespaces/shop/persistentvolumeclaims/data", js(t, bound), js(t, bound)); !d.Allow {
		t.Errorf("updating a bound claim refused: %s", d.Reason)
	}
	moved := corev1.PersistentVolumeClaim{Spec: corev1.PersistentVolumeClaimSpec{VolumeName: "pvc-2"}}
	if d := decide(t, cluster, "PUT", "/api/v1/namespaces/shop/persistentvolumeclaims/data", js(t, moved), js(t, bound)); d.Allow {
		t.Error("pointing a claim at another volume was allowed")
	}
	// Deletes need no object.
	for _, p := range []string{"/apis/apps/v1/namespaces/shop/deployments/web", "/api/v1/namespaces/shop/persistentvolumeclaims/data", "/api/v1/namespaces/shop/pods/p"} {
		if d := Decide(Request{Method: "DELETE", Path: p, Now: now}, cluster); !d.Allow {
			t.Errorf("DELETE %s refused: %s", p, d.Reason)
		}
	}
}

func TestNamespaces(t *testing.T) {
	ns := func(name string, labels map[string]string) []byte {
		return js(t, corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels}})
	}
	managed := map[string]string{ManagedLabel: ManagedValue}
	cases := []struct {
		name   string
		method string
		path   string
		stored []byte
		allow  bool
	}{
		{"create Meshploy's", "POST", "/api/v1/namespaces", ns("blog", managed), true},
		{"create unlabelled", "POST", "/api/v1/namespaces", ns("blog", nil), false},
		{"create kube-like", "POST", "/api/v1/namespaces", ns("kube-evil", managed), false},
		{"create default", "POST", "/api/v1/namespaces", ns("default", managed), false},
		{"delete Meshploy's", "DELETE", "/api/v1/namespaces/shop", nil, true},
		{"delete kube-system", "DELETE", "/api/v1/namespaces/kube-system", nil, false},
		{"delete someone's", "DELETE", "/api/v1/namespaces/someone-else", nil, false},
		{"delete all", "DELETE", "/api/v1/namespaces", nil, false},
		{"relabel default", "PATCH", "/api/v1/namespaces/default", ns("default", managed), false},
		{"update Meshploy's", "PUT", "/api/v1/namespaces/shop", ns("shop", managed), false},
		{"finalize", "PUT", "/api/v1/namespaces/shop/finalize", ns("shop", managed), false},
		{"status", "PUT", "/api/v1/namespaces/shop/status", ns("shop", managed), false},
	}
	for _, c := range cases {
		d := Decide(Request{Method: c.method, Path: c.path, Now: now}, cluster)
		if d.NeedObject {
			d = Decide(Request{Method: c.method, Path: c.path, Stored: c.stored, Now: now}, cluster)
		}
		if d.Allow != c.allow {
			t.Errorf("%s: allow=%v (%s), want %v", c.name, d.Allow, d.Reason, c.allow)
		}
	}
}

func TestNodes(t *testing.T) {
	base := corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "edge", Labels: map[string]string{"kubernetes.io/hostname": "edge"},
			Annotations: map[string]string{"flannel.alpha.coreos.com/public-ip": "100.64.0.2"}},
		Spec: corev1.NodeSpec{PodCIDR: "10.42.0.0/24"},
	}
	change := func(f func(*corev1.Node)) []byte {
		n := *base.DeepCopy()
		f(&n)
		return js(t, n)
	}
	ours := corev1.Taint{Key: RoleKey, Value: "builder", Effect: corev1.TaintEffectNoSchedule}
	current := js(t, base)
	cases := []struct {
		name   string
		stored []byte
		allow  bool
	}{
		{"add the builder label", change(func(n *corev1.Node) { n.Labels[RoleKey] = "builder" }), true},
		{"builder label and taint", change(func(n *corev1.Node) { n.Labels[RoleKey] = "builder"; n.Spec.Taints = []corev1.Taint{ours} }), true},
		{"nothing", current, true},
		{"another label", change(func(n *corev1.Node) { n.Labels["node-role.kubernetes.io/control-plane"] = "true" }), false},
		{"role label with another value", change(func(n *corev1.Node) { n.Labels[RoleKey] = "admin" }), false},
		{"an annotation", change(func(n *corev1.Node) { n.Annotations["flannel.alpha.coreos.com/public-ip"] = "6.6.6.6" }), false},
		{"another taint", change(func(n *corev1.Node) { n.Spec.Taints = []corev1.Taint{{Key: "x", Effect: corev1.TaintEffectNoExecute}} }), false},
		{"role taint NoExecute", change(func(n *corev1.Node) {
			n.Spec.Taints = []corev1.Taint{{Key: RoleKey, Value: "builder", Effect: corev1.TaintEffectNoExecute}}
		}), false},
		{"unschedulable", change(func(n *corev1.Node) { n.Spec.Unschedulable = true }), false},
		{"pod CIDR", change(func(n *corev1.Node) { n.Spec.PodCIDR = "10.99.0.0/24" }), false},
		{"finalizer", change(func(n *corev1.Node) { n.Finalizers = []string{"x"} }), false},
	}
	for _, c := range cases {
		for _, method := range []string{"PATCH", "PUT"} {
			if d := decide(t, cluster, method, "/api/v1/nodes/edge", c.stored, current); d.Allow != c.allow {
				t.Errorf("%s %s: allow=%v (%s), want %v", method, c.name, d.Allow, d.Reason, c.allow)
			}
		}
	}
	// Removing Meshploy's taint and label from a builder node.
	builder := change(func(n *corev1.Node) { n.Labels[RoleKey] = "builder"; n.Spec.Taints = []corev1.Taint{ours} })
	if d := decide(t, cluster, "PATCH", "/api/v1/nodes/edge", current, builder); !d.Allow {
		t.Errorf("clearing the role refused: %s", d.Reason)
	}
	for _, c := range []struct{ method, path string }{
		{"POST", "/api/v1/nodes"},
		{"DELETE", "/api/v1/nodes/edge"},
		{"DELETE", "/api/v1/nodes"},
		{"PATCH", "/api/v1/nodes/edge/status"},
		{"PUT", "/api/v1/nodes/edge/status"},
		{"POST", "/api/v1/nodes/edge/proxy/x"},
		{"GET", "/api/v1/nodes/edge/proxy/configz"},
	} {
		d := Decide(Request{Method: c.method, Path: c.path, Now: now}, cluster)
		if d.NeedObject {
			t.Errorf("%s %s asked for its object", c.method, c.path)
		}
		if d.Allow && c.method != "GET" {
			t.Errorf("%s %s allowed: %s", c.method, c.path, d.Reason)
		}
	}
}

func TestOtherClusterWideWritesAreRefused(t *testing.T) {
	for _, p := range []string{
		"/api/v1/persistentvolumes",
		"/apis/rbac.authorization.k8s.io/v1/clusterrolebindings",
		"/apis/rbac.authorization.k8s.io/v1/clusterroles",
		"/apis/apiextensions.k8s.io/v1/customresourcedefinitions",
		"/apis/admissionregistration.k8s.io/v1/validatingwebhookconfigurations",
		"/apis/admissionregistration.k8s.io/v1/mutatingwebhookconfigurations",
		"/apis/storage.k8s.io/v1/storageclasses",
		"/apis/certificates.k8s.io/v1/certificatesigningrequests",
		"/apis/scheduling.k8s.io/v1/priorityclasses",
		"/apis/authentication.k8s.io/v1/tokenreviews",
	} {
		for _, m := range []string{"POST", "PUT", "PATCH", "DELETE"} {
			path := p
			if m != "POST" {
				path += "/x"
			}
			if d := decide(t, cluster, m, path, []byte(`{}`), []byte(`{}`)); d.Allow {
				t.Errorf("%s %s allowed: %s", m, path, d.Reason)
			}
		}
	}
	for _, p := range []string{"/version", "/healthz", "/apis", "/openapi/v2"} {
		if d := Decide(Request{Method: "POST", Path: p, Now: now}, cluster); d.Allow {
			t.Errorf("POST %s allowed", p)
		}
		if d := Decide(Request{Method: "GET", Path: p, Now: now}, cluster); !d.Allow {
			t.Errorf("GET %s refused", p)
		}
	}
}

func TestKubeSystem(t *testing.T) {
	cases := []struct {
		name    string
		f       facts
		method  string
		path    string
		upgrade bool
		allow   bool
	}{
		{"exec into the terminal, approved", approved(), "POST", "/api/v1/namespaces/kube-system/pods/meshploy-shell-1/exec", true, true},
		{"attach to the terminal, approved", approved(), "POST", "/api/v1/namespaces/kube-system/pods/meshploy-shell-1/attach", true, true},
		{"exec into the terminal, not approved", cluster, "POST", "/api/v1/namespaces/kube-system/pods/meshploy-shell-1/exec", true, false},
		{"exec into the terminal, approval over", func() facts { f := cluster; f.approved = now.Add(-time.Second); return f }(), "POST", "/api/v1/namespaces/kube-system/pods/meshploy-shell-1/exec", true, false},
		{"exec into coredns, approved", approved(), "POST", "/api/v1/namespaces/kube-system/pods/coredns-1/exec", true, false},
		{"port-forward to the terminal, approved", approved(), "POST", "/api/v1/namespaces/kube-system/pods/meshploy-shell-1/portforward", true, false},
		{"delete coredns", approved(), "DELETE", "/api/v1/namespaces/kube-system/pods/coredns-1", false, false},
		{"delete the terminal", approved(), "DELETE", "/api/v1/namespaces/kube-system/pods/meshploy-shell-1", false, false},
		{"create a pod", approved(), "POST", "/api/v1/namespaces/kube-system/pods", false, false},
		{"a helm chart", approved(), "POST", "/apis/helm.cattle.io/v1/namespaces/kube-system/helmcharts", false, false},
		{"read the terminal's logs", cluster, "GET", "/api/v1/namespaces/kube-system/pods/meshploy-shell-1/log", false, true},
		{"read secrets", approved(), "GET", "/api/v1/namespaces/kube-system/secrets", false, false},
	}
	for _, c := range cases {
		d := Decide(Request{Method: c.method, Path: c.path, Upgrade: c.upgrade, Now: now}, c.f)
		if d.Allow != c.allow || d.NeedObject {
			t.Errorf("%s: allow=%v need=%v (%s), want %v", c.name, d.Allow, d.NeedObject, d.Reason, c.allow)
		}
	}
}

func TestReads(t *testing.T) {
	cases := []struct {
		path  string
		allow bool
	}{
		{"/api/v1/namespaces/shop/secrets", true},
		{"/api/v1/namespaces/shop/secrets/cfg", true},
		{"/api/v1/secrets", false},
		{"/api/v1/namespaces/default/secrets/x", false},
		{"/api/v1/namespaces/kube-system/secrets/k3s-serving", false},
		{"/api/v1/pods", true},
		{"/api/v1/nodes", true},
		{"/apis/apps/v1/namespaces/shop/deployments?watch=1", true},
		{"/api/v1/namespaces/shop/pods/p/log", true},
	}
	for _, c := range cases {
		for _, m := range []string{"GET", "HEAD"} {
			if d := Decide(Request{Method: m, Path: c.path, Now: now}, cluster); d.Allow != c.allow {
				t.Errorf("%s %s: allow=%v (%s), want %v", m, c.path, d.Allow, d.Reason, c.allow)
			}
		}
	}
}

// Unreadable stored objects are refused, not waved through.
func TestUnreadableStoredObjects(t *testing.T) {
	for _, p := range []string{
		"/api/v1/namespaces/shop/pods",
		"/apis/apps/v1/namespaces/shop/deployments",
		"/api/v1/namespaces/shop/services",
		"/api/v1/namespaces/shop/persistentvolumeclaims",
		"/api/v1/namespaces",
	} {
		if d := decide(t, cluster, "POST", p, []byte(`{not json`), nil); d.Allow {
			t.Errorf("POST %s with an unreadable stored object allowed", p)
		}
	}
	if d := decide(t, cluster, "PATCH", "/api/v1/nodes/edge", []byte(`{not json`), []byte(`{}`)); d.Allow {
		t.Error("node patch with an unreadable stored object allowed")
	}
}

// Path tricks: odd spellings of a path must not land in a looser branch.
func TestPathShapes(t *testing.T) {
	for _, c := range []struct {
		method, path string
	}{
		{"POST", "/api/v1/namespaces/kube-system/pods/"},
		{"POST", "//api//v1//namespaces//kube-system//pods"},
		{"DELETE", "/api/v1/namespaces/kube-system"},
		{"POST", "/apis/apps/v1/namespaces/kube-system/deployments"},
		{"POST", "/api/v1/namespaces/shop/../kube-system/pods"},
	} {
		d := decide(t, cluster, c.method, c.path, js(t, pod(app())), nil)
		if d.Allow {
			t.Errorf("%s %s allowed: %s", c.method, c.path, d.Reason)
		}
	}
}

func TestReasonsNameTheRisk(t *testing.T) {
	d := decide(t, cluster, "POST", "/api/v1/namespaces/shop/pods", js(t, pod(attacks()["host path"])), nil)
	if d.Allow || !strings.Contains(d.Reason, "host") {
		t.Errorf("reason %q does not say what the pod reaches", d.Reason)
	}
}

func TestRuntimeClasses(t *testing.T) {
	rc := func(name, handler string, sel map[string]string) []byte {
		v := nodev1.RuntimeClass{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{"managed-by": "meshploy"}}, Handler: handler}
		if sel != nil {
			v.Scheduling = &nodev1.Scheduling{NodeSelector: sel}
		}
		return js(t, v)
	}
	sandboxNodes := map[string]string{"meshploy.com/sandbox": "true"}
	cases := []struct {
		name, method, path string
		stored             []byte
		allow              bool
	}{
		{"create gVisor's", "POST", "/apis/node.k8s.io/v1/runtimeclasses", rc("gvisor", "runsc", sandboxNodes), true},
		{"update gVisor's", "PUT", "/apis/node.k8s.io/v1/runtimeclasses/gvisor", rc("gvisor", "runsc", sandboxNodes), true},
		{"delete gVisor's", "DELETE", "/apis/node.k8s.io/v1/runtimeclasses/gvisor", nil, true},
		{"gvisor on runc", "POST", "/apis/node.k8s.io/v1/runtimeclasses", rc("gvisor", "runc", sandboxNodes), false},
		{"gvisor anywhere", "POST", "/apis/node.k8s.io/v1/runtimeclasses", rc("gvisor", "runsc", nil), false},
		{"gvisor on other nodes", "POST", "/apis/node.k8s.io/v1/runtimeclasses", rc("gvisor", "runsc", map[string]string{"kubernetes.io/hostname": "edge"}), false},
		{"another class", "POST", "/apis/node.k8s.io/v1/runtimeclasses", rc("nvidia", "nvidia", nil), false},
		{"update another", "PUT", "/apis/node.k8s.io/v1/runtimeclasses/nvidia", rc("nvidia", "runsc", sandboxNodes), false},
		{"delete another", "DELETE", "/apis/node.k8s.io/v1/runtimeclasses/nvidia", nil, false},
		{"delete all", "DELETE", "/apis/node.k8s.io/v1/runtimeclasses", nil, false},
	}
	for _, c := range cases {
		if d := decide(t, cluster, c.method, c.path, c.stored, c.stored); d.Allow != c.allow {
			t.Errorf("%s: allow=%v (%s), want %v", c.name, d.Allow, d.Reason, c.allow)
		}
	}
}

// Package policy decides which Kubernetes requests from the API the cluster
// agent lets through to the cluster it holds.
//
// The API may read what Meshploy shows, and change only what Meshploy
// creates: a fixed list of kinds, in namespaces labelled managed-by=meshploy,
// never reaching the host. The pods Meshploy needs that do reach the host
// (the builder, the image cleanup, the node terminal) are never accepted as
// objects at all: the API asks for them by intent and the agent makes them
// from its own templates. Exec reaches a kube-system pod only when it is a
// node terminal the agent made, and only while the machine's owner has
// approved it.
//
// A write is judged by the object the API server would store, not by the
// body sent: a patch carries only part of an object, and defaults and
// admission change the rest. Such a write is sent as a server-side dry run
// first, and decided again with the result.
package policy

import (
	"encoding/json"
	"fmt"
	"net/http"
	pathpkg "path"
	"reflect"
	"strings"
	"time"

	appk8s "github.com/meshploy/packages/server/k8s"
	corev1 "k8s.io/api/core/v1"
	nodev1 "k8s.io/api/node/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	psaapi "k8s.io/pod-security-admission/api"
	psapolicy "k8s.io/pod-security-admission/policy"
)

// ManagedLabel is the label on every namespace Meshploy creates, and the only
// namespaces the API may change anything in.
const (
	ManagedLabel = "managed-by"
	ManagedValue = "meshploy"
)

// RoleKey is the node label and taint key through which Meshploy says which
// nodes build. It is the only thing on a node the API may change.
const RoleKey = appk8s.BuilderNodeLabel

// Decision is the answer for one request.
type Decision struct {
	Allow  bool
	Reason string
	// NeedObject asks for the request's dry-run result in Request.Stored, and
	// for an update the object as it is now in Request.Current, before an
	// answer can be given.
	NeedObject bool
}

func allow(r string) Decision { return Decision{Allow: true, Reason: r} }
func deny(r string) Decision  { return Decision{Reason: r} }

var needObject = Decision{Reason: "the object it would store is needed", NeedObject: true}

// Facts are what the policy asks the cluster and the machine's owner.
type Facts interface {
	// Managed is whether a namespace carries managed-by=meshploy.
	Managed(ns string) bool
	// TerminalApprovedUntil is when the owner's approval to open a node
	// terminal ends; zero when there is none.
	TerminalApprovedUntil() time.Time
	// Shell is whether a kube-system pod is a node terminal the agent made.
	Shell(name string) bool
}

// Request is the part of an HTTP request the policy reads.
type Request struct {
	Method string
	Path   string
	// Upgrade is set for a request asking to switch protocols: exec, attach
	// and port-forward.
	Upgrade bool
	// Stored is the object the API server would store for this write, from
	// its server-side dry run. Nil until a Decision asks for it.
	Stored []byte
	// Current is the object as it is before an update (PUT or PATCH). Nil
	// until a Decision asks for it.
	Current []byte
	// Now is the time decisions are taken at; zero means time.Now().
	Now time.Time
}

// path is a Kubernetes API path taken apart.
type path struct {
	api            bool
	group, version string
	namespace      string
	resource, name string
	subresource    string
	clusterScoped  bool
}

func parse(p string) path {
	parts := strings.Split(strings.Trim(p, "/"), "/")
	var out path
	switch {
	case len(parts) >= 2 && parts[0] == "api":
		out.api, out.version, parts = true, parts[1], parts[2:]
	case len(parts) >= 3 && parts[0] == "apis":
		out.api, out.group, out.version, parts = true, parts[1], parts[2], parts[3:]
	default:
		return out // discovery, /version, /healthz, /openapi
	}
	// namespaces/<ns>/<resource>... is namespaced; namespaces and
	// namespaces/<name>[/<sub>] are the namespace objects themselves.
	if len(parts) >= 3 && parts[0] == "namespaces" {
		out.namespace, parts = parts[1], parts[2:]
	} else {
		out.clusterScoped = true
	}
	if len(parts) > 0 {
		out.resource = parts[0]
	}
	if len(parts) > 1 {
		out.name = parts[1]
	}
	if len(parts) > 2 {
		out.subresource = strings.Join(parts[2:], "/")
	}
	return out
}

// kind is a group and resource, the way RBAC names them.
type kind struct{ group, resource string }

// writable is every kind the API writes in a Meshploy namespace: the
// workloads it runs, how they are reached and configured, their storage,
// isolation and quotas. Nothing else may be written there; a kind not on the
// list may make pods or grant rights in ways no check below can see (k3s's
// HelmChart runs an installer as cluster-admin, a RoleBinding hands a pod the
// API server directly).
var writable = map[kind]bool{
	{"", "pods"}:                             true,
	{"", "services"}:                         true,
	{"", "secrets"}:                          true,
	{"", "persistentvolumeclaims"}:           true,
	{"", "resourcequotas"}:                   true,
	{"", "limitranges"}:                      true,
	{"apps", "deployments"}:                  true,
	{"batch", "jobs"}:                        true,
	{"batch", "cronjobs"}:                    true,
	{"networking.k8s.io", "networkpolicies"}: true,
}

// podTemplated is every writable kind that carries a pod.
var podTemplated = map[kind]bool{
	{"", "pods"}:            true,
	{"apps", "deployments"}: true,
	{"batch", "jobs"}:       true,
	{"batch", "cronjobs"}:   true,
}

// Decide is the answer for one request from the API.
func Decide(r Request, f Facts) Decision {
	now := r.Now
	if now.IsZero() {
		now = time.Now()
	}
	// A path is judged as written; one the API server would read differently
	// (dot segments, doubled slashes) is not judged at all.
	if clean := pathpkg.Clean("/" + r.Path); clean != r.Path && clean+"/" != r.Path {
		return deny("a path not in its plain form")
	}
	p := parse(r.Path)
	write := r.Method != http.MethodGet && r.Method != http.MethodHead
	// Exec, attach and port-forward run code in a pod or reach into it: a
	// write, whatever the method.
	streaming := p.resource == "pods" && (p.subresource == "exec" || p.subresource == "attach" || p.subresource == "portforward")
	if streaming || r.Upgrade {
		write = true
	}
	if !p.api {
		if write {
			return deny("a write outside the resource API")
		}
		return allow("discovery")
	}
	if !write {
		return decideRead(p, f)
	}
	if p.clusterScoped {
		return decideClusterScoped(r, p, f)
	}
	if p.namespace == "kube-system" {
		return decideKubeSystem(r, p, f, now, streaming)
	}
	if !f.Managed(p.namespace) {
		return deny(fmt.Sprintf("a write in namespace %s, which Meshploy did not create", p.namespace))
	}
	return decideManaged(r, p, streaming)
}

func decideRead(p path, f Facts) Decision {
	// Secrets outside Meshploy's namespaces hold the cluster's own keys.
	if p.resource == "secrets" && p.group == "" && (p.clusterScoped || !f.Managed(p.namespace)) {
		return deny("reading secrets outside Meshploy's namespaces")
	}
	return allow("read")
}

func decideClusterScoped(r Request, p path, f Facts) Decision {
	switch {
	case p.group == "" && p.resource == "namespaces" && p.subresource == "":
		switch {
		case r.Method == http.MethodPost && p.name == "":
			if r.Stored == nil {
				return needObject
			}
			var ns corev1.Namespace
			if err := json.Unmarshal(r.Stored, &ns); err != nil {
				return deny("a namespace whose stored form cannot be read")
			}
			if ns.Labels[ManagedLabel] != ManagedValue {
				return deny("a namespace not labelled " + ManagedLabel + "=" + ManagedValue)
			}
			if strings.HasPrefix(ns.Name, "kube-") || ns.Name == "default" {
				return deny("a namespace named like the cluster's own")
			}
			return allow("a Meshploy namespace")
		case r.Method == http.MethodDelete && p.name != "":
			if !f.Managed(p.name) {
				return deny("deleting namespace " + p.name + ", which Meshploy did not create")
			}
			return allow("deleting a Meshploy namespace")
		}
	case p.group == "" && p.resource == "nodes" && p.name != "" && p.subresource == "" &&
		(r.Method == http.MethodPatch || r.Method == http.MethodPut):
		if r.Stored == nil || r.Current == nil {
			return needObject
		}
		if what := nodeChangeBeyondRole(r.Current, r.Stored); what != "" {
			return deny("a node change beyond Meshploy's role label and taint: " + what)
		}
		return allow("setting a node's build role")
	case p.group == "" && p.resource == "nodes":
		return deny("adding or removing a node, or changing its status, is the machine's own business")
	case p.group == "node.k8s.io" && p.resource == "runtimeclasses" && p.subresource == "":
		return decideRuntimeClass(r, p)
	}
	return deny(fmt.Sprintf("a cluster-wide %s on %s", r.Method, groupResource(p)))
}

// The one RuntimeClass the API keeps is gVisor's, scheduled onto the nodes
// that have it.
const (
	SandboxRuntimeClass = appk8s.SandboxRuntimeClass
	sandboxHandler      = "runsc"
	sandboxNodeLabel    = appk8s.SandboxNodeLabel
)

func decideRuntimeClass(r Request, p path) Decision {
	if r.Method == http.MethodDelete {
		if p.name == SandboxRuntimeClass {
			return allow("removing the sandbox runtime class, to make it again")
		}
		return deny("removing runtime class " + p.name)
	}
	if r.Method == http.MethodPost && p.name != "" || r.Method != http.MethodPost && p.name != SandboxRuntimeClass {
		return deny("a runtime class other than the sandbox's")
	}
	if r.Stored == nil {
		return needObject
	}
	var rc nodev1.RuntimeClass
	if err := json.Unmarshal(r.Stored, &rc); err != nil {
		return deny("a runtime class whose stored form cannot be read")
	}
	want := map[string]string{sandboxNodeLabel: "true"}
	if rc.Name != SandboxRuntimeClass || rc.Handler != sandboxHandler || rc.Overhead != nil ||
		rc.Scheduling == nil || len(rc.Scheduling.Tolerations) != 0 || !reflect.DeepEqual(rc.Scheduling.NodeSelector, want) {
		return deny("a runtime class other than gVisor's on the sandbox nodes")
	}
	return allow("the sandbox runtime class")
}

func decideKubeSystem(r Request, p path, f Facts, now time.Time, streaming bool) Decision {
	if p.group == "" && p.resource == "pods" && p.name != "" && (p.subresource == "exec" || p.subresource == "attach") {
		if !f.Shell(p.name) {
			return deny("into a kube-system pod that is not a node terminal")
		}
		if now.Before(f.TerminalApprovedUntil()) {
			return allow("into the node terminal, approved")
		}
		return deny("into the node terminal without the owner's approval")
	}
	if streaming || r.Upgrade {
		return deny("into a kube-system pod")
	}
	// Meshploy's own pods here are made and removed by intent.
	return deny("a write in kube-system")
}

func decideManaged(r Request, p path, streaming bool) Decision {
	k := kind{p.group, p.resource}
	if !writable[k] {
		return deny("a write to " + groupResource(p) + ", which Meshploy does not make")
	}
	if streaming {
		return allow("into a pod in a Meshploy namespace")
	}
	if r.Upgrade {
		return deny("an upgrade outside exec, attach and port-forward")
	}
	switch p.subresource {
	case "":
	case "scale":
		if k == (kind{"apps", "deployments"}) {
			return allow("scaling a deployment")
		}
		return deny("scaling " + groupResource(p))
	case "status":
		return deny("writing the status of " + groupResource(p) + ", which its controller owns")
	default:
		return deny("a write to " + groupResource(p) + "/" + p.subresource)
	}
	if r.Method == http.MethodDelete {
		return allow("deleting " + groupResource(p))
	}
	if r.Stored == nil {
		return needObject
	}
	switch {
	case podTemplated[k]:
		meta, spec, err := podOf(p.resource, r.Stored)
		if err != nil {
			return deny("a workload whose stored form cannot be read: " + err.Error())
		}
		if risk := hostRisk(meta, spec); risk != "" {
			return deny("a workload that " + risk)
		}
	case k == kind{"", "services"}:
		if risk := serviceRisk(r.Stored); risk != "" {
			return deny("a service that " + risk)
		}
	case k == kind{"", "persistentvolumeclaims"}:
		var c corev1.PersistentVolumeClaim
		if err := json.Unmarshal(r.Stored, &c); err != nil {
			return deny("a claim whose stored form cannot be read")
		}
		// A claim naming its volume can take one another claim released.
		if c.Spec.VolumeName != "" && (r.Current == nil || volumeNameOf(r.Current) != c.Spec.VolumeName) {
			return deny("a claim naming the volume it binds to")
		}
	}
	return allow("a change in a Meshploy namespace")
}

func groupResource(p path) string {
	if p.group == "" {
		return p.resource
	}
	return p.resource + "." + p.group
}

func volumeNameOf(raw []byte) string {
	var c corev1.PersistentVolumeClaim
	_ = json.Unmarshal(raw, &c)
	return c.Spec.VolumeName
}

// podOf is the pod, or the pod template, in a stored workload.
func podOf(resource string, body []byte) (*metav1.ObjectMeta, *corev1.PodSpec, error) {
	switch resource {
	case "pods":
		var pod corev1.Pod
		if err := json.Unmarshal(body, &pod); err != nil {
			return nil, nil, err
		}
		return &pod.ObjectMeta, &pod.Spec, nil
	case "cronjobs":
		var c struct {
			Spec struct {
				JobTemplate struct {
					Spec struct {
						Template corev1.PodTemplateSpec `json:"template"`
					} `json:"spec"`
				} `json:"jobTemplate"`
			} `json:"spec"`
		}
		if err := json.Unmarshal(body, &c); err != nil {
			return nil, nil, err
		}
		t := c.Spec.JobTemplate.Spec.Template
		return &t.ObjectMeta, &t.Spec, nil
	default:
		var w struct {
			Spec struct {
				Template corev1.PodTemplateSpec `json:"template"`
			} `json:"spec"`
		}
		if err := json.Unmarshal(body, &w); err != nil {
			return nil, nil, err
		}
		t := w.Spec.Template
		return &t.ObjectMeta, &t.Spec, nil
	}
}

var baseline = psapolicy.DefaultChecks()

// hostRisk says how a pod reaches past its container, or "" when it does not.
// Kubernetes' own Pod Security "baseline" checks come first, then what
// Meshploy refuses beyond them.
func hostRisk(meta *metav1.ObjectMeta, s *corev1.PodSpec) string {
	eval, err := psapolicy.NewEvaluator(baseline)
	if err != nil {
		return "cannot be checked: " + err.Error()
	}
	level := psaapi.LevelVersion{Level: psaapi.LevelBaseline, Version: psaapi.LatestVersion()}
	if res := psapolicy.AggregateCheckResults(eval.EvaluatePod(level, meta, s)); !res.Allowed {
		return "breaks the baseline Pod Security standard: " + res.ForbiddenDetail()
	}
	for _, v := range s.Volumes {
		if !plainVolume(v.VolumeSource) {
			return "mounts a volume of a kind that reaches outside the pod (" + v.Name + ")"
		}
	}
	if ps := s.SecurityContext; ps != nil {
		if localProfile(ps.SeccompProfile, ps.AppArmorProfile) {
			return "uses a security profile from a file on the node"
		}
	}
	for _, c := range allContainers(s) {
		sc := c.SecurityContext
		if sc == nil {
			continue
		}
		// Baseline lets a pod add a few capabilities; Meshploy's workloads add
		// none.
		if sc.Capabilities != nil && len(sc.Capabilities.Add) > 0 {
			return "adds capabilities (" + c.Name + ")"
		}
		if localProfile(sc.SeccompProfile, sc.AppArmorProfile) {
			return "uses a security profile from a file on the node (" + c.Name + ")"
		}
	}
	return ""
}

// plainVolume is a volume kind that holds only what the pod is given: no
// path, device or network mount on the node.
func plainVolume(v corev1.VolumeSource) bool {
	n := 0
	for _, set := range []bool{
		v.EmptyDir != nil, v.Secret != nil, v.ConfigMap != nil, v.PersistentVolumeClaim != nil,
		v.Projected != nil, v.DownwardAPI != nil, v.Ephemeral != nil,
	} {
		if set {
			n++
		}
	}
	// Exactly one of the plain kinds, and nothing else: a source with a second
	// field set is refused by the API server, and one with none set is a kind
	// this list does not know.
	if n != 1 {
		return false
	}
	plain := corev1.VolumeSource{
		EmptyDir: v.EmptyDir, Secret: v.Secret, ConfigMap: v.ConfigMap, PersistentVolumeClaim: v.PersistentVolumeClaim,
		Projected: v.Projected, DownwardAPI: v.DownwardAPI, Ephemeral: v.Ephemeral,
	}
	return reflect.DeepEqual(plain, v)
}

func localProfile(s *corev1.SeccompProfile, a *corev1.AppArmorProfile) bool {
	return (s != nil && s.Type == corev1.SeccompProfileTypeLocalhost) ||
		(a != nil && a.Type == corev1.AppArmorProfileTypeLocalhost)
}

// allContainers is every container a pod runs, ephemeral ones included: one
// added to a running pod gets the same reach as the pod's own.
func allContainers(s *corev1.PodSpec) []corev1.Container {
	out := append(append([]corev1.Container{}, s.InitContainers...), s.Containers...)
	for _, e := range s.EphemeralContainers {
		out = append(out, corev1.Container(e.EphemeralContainerCommon))
	}
	return out
}

// serviceRisk says how a service reaches past the cluster's own addresses.
func serviceRisk(raw []byte) string {
	var s corev1.Service
	if err := json.Unmarshal(raw, &s); err != nil {
		return "cannot be read"
	}
	switch s.Spec.Type {
	case corev1.ServiceTypeLoadBalancer:
		return "asks for a load balancer, which binds the node's ports"
	case corev1.ServiceTypeExternalName:
		return "names an outside host"
	}
	if len(s.Spec.ExternalIPs) > 0 {
		// Traffic to these addresses is captured on every node.
		return "claims external addresses"
	}
	return ""
}

// nodeChangeBeyondRole says what an update changes on a node besides
// Meshploy's role label and taint, or "" when it changes nothing else.
func nodeChangeBeyondRole(current, stored []byte) string {
	var before, after corev1.Node
	if json.Unmarshal(current, &before) != nil || json.Unmarshal(stored, &after) != nil {
		return "the node cannot be read"
	}
	if before.Name != after.Name {
		return "its name"
	}
	if !reflect.DeepEqual(withoutKey(before.Labels), withoutKey(after.Labels)) {
		return "its labels"
	}
	if !reflect.DeepEqual(emptyAsNil(before.Annotations), emptyAsNil(after.Annotations)) {
		return "its annotations"
	}
	if !reflect.DeepEqual(before.OwnerReferences, after.OwnerReferences) ||
		!reflect.DeepEqual(before.Finalizers, after.Finalizers) {
		return "its owners or finalizers"
	}
	bs, as := before.Spec, after.Spec
	bs.Taints, as.Taints = withoutRoleTaints(bs.Taints), withoutRoleTaints(as.Taints)
	if !reflect.DeepEqual(bs, as) {
		return "its spec"
	}
	for _, t := range after.Spec.Taints {
		if t.Key == RoleKey && (t.Effect != corev1.TaintEffectNoSchedule || t.Value != appk8s.BuilderNodeValue) {
			return "a role taint other than builder:NoSchedule"
		}
	}
	if v, ok := after.Labels[RoleKey]; ok && v != appk8s.BuilderNodeValue {
		return "a role label other than builder"
	}
	return ""
}

func withoutKey(m map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range m {
		if k != RoleKey {
			out[k] = v
		}
	}
	return out
}

func emptyAsNil(m map[string]string) map[string]string {
	if len(m) == 0 {
		return nil
	}
	return m
}

func withoutRoleTaints(ts []corev1.Taint) []corev1.Taint {
	var out []corev1.Taint
	for _, t := range ts {
		if t.Key != RoleKey {
			// The API server stamps TimeAdded on some taints; it is not a change
			// the request asked for.
			t.TimeAdded = nil
			out = append(out, t)
		}
	}
	return out
}

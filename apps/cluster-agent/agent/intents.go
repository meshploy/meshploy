package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	appk8s "github.com/meshploy/packages/server/k8s"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// IntentPrefix is where the API asks for the pods that reach the host. The
// paths under it, and their bodies, are what appk8s.AgentDriver sends.
const IntentPrefix = appk8s.IntentPrefix

// pruneTimeout bounds one image cleanup.
const pruneTimeout = 90 * time.Second

// shellName is the only name a node terminal's pod may take.
var shellName = regexp.MustCompile(`^meshploy-shell-[a-z0-9]{1,40}$`)

// errRefused is a request the agent will not carry out; its text is the
// reason.
type errRefused struct{ reason string }

func (e errRefused) Error() string { return e.reason }

func (a *Agent) serveIntent(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.EscapedPath(), IntentPrefix)
	if host, ok := strings.CutPrefix(rest, "host/"); ok {
		a.serveHost(w, r, host)
		return
	}
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, IntentPrefix), "/"), "/")
	ctx := r.Context()
	var (
		out  any
		what string
		err  error
	)
	switch {
	case r.Method == http.MethodPost && len(parts) == 1 && parts[0] == "builds":
		what, err = a.build(ctx, r.Body)
	case r.Method == http.MethodPost && len(parts) == 3 && parts[0] == "nodes" && parts[2] == "prune":
		var n int
		n, err = a.prune(ctx, parts[1])
		out, what = appk8s.PruneResult{Removed: n}, "the image cleanup on "+parts[1]
	case r.Method == http.MethodPost && len(parts) == 4 && parts[0] == "nodes" && parts[2] == "shells":
		out, err = a.openShell(ctx, parts[1], parts[3])
		what = "the node terminal on " + parts[1]
	case r.Method == http.MethodDelete && len(parts) == 2 && parts[0] == "shells":
		what, err = a.closeShell(ctx, parts[1])
	default:
		a.refuseIntent(w, r, http.StatusNotFound, "no such request")
		return
	}
	var refused errRefused
	switch {
	case errors.As(err, &refused):
		a.refuseIntent(w, r, http.StatusForbidden, refused.reason)
		return
	case err != nil:
		a.logf("FAIL  %s %s: %v", r.Method, r.URL.Path, err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	a.logf("DO    %s %s: %s", r.Method, r.URL.Path, what)
	w.Header().Set("Content-Type", "application/json")
	if out == nil {
		out = map[string]string{"done": what}
	}
	json.NewEncoder(w).Encode(out)
}

func (a *Agent) refuseIntent(w http.ResponseWriter, r *http.Request, status int, reason string) {
	a.logf("DENY  %s %s: %s", r.Method, r.URL.Path, reason)
	http.Error(w, "the cluster agent refused this: "+reason, status)
}

// build starts a build from the API's inputs. The builder is the agent's:
// whatever image the API names is replaced, so the API chooses what is
// built, never what builds it.
func (a *Agent) build(ctx context.Context, body io.Reader) (string, error) {
	var p appk8s.BuildJobParams
	dec := json.NewDecoder(io.LimitReader(body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return "", errRefused{"a build whose inputs cannot be read: " + err.Error()}
	}
	if a.builds.Image == "" {
		return "", errRefused{"builds, which this cluster agent is not set up to run"}
	}
	if p.JobName == "" || p.Namespace == "" {
		return "", errRefused{"a build without a name or namespace"}
	}
	if !a.facts.Managed(p.Namespace) {
		return "", errRefused{fmt.Sprintf("a build in namespace %q, which Meshploy did not create", p.Namespace)}
	}
	asked := p.Image
	p.Image = a.builds.Image
	if err := appk8s.CreateBuildJob(ctx, a.cs, p); err != nil {
		return "", err
	}
	if asked != "" && asked != p.Image {
		return fmt.Sprintf("build %s/%s, with the agent's builder (%s named)", p.Namespace, p.JobName, asked), nil
	}
	return fmt.Sprintf("build %s/%s", p.Namespace, p.JobName), nil
}

func (a *Agent) prune(ctx context.Context, node string) (int, error) {
	if _, err := a.cs.CoreV1().Nodes().Get(ctx, node, metav1.GetOptions{}); err != nil {
		if k8serrors.IsNotFound(err) {
			return 0, errRefused{"a cleanup on " + node + ", which is not in this cluster"}
		}
		return 0, err
	}
	return appk8s.PruneNodeImages(ctx, a.cs, node, pruneTimeout)
}

func (a *Agent) openShell(ctx context.Context, node, name string) (*corev1.Pod, error) {
	if !shellName.MatchString(name) {
		return nil, errRefused{"a node terminal named " + name}
	}
	if !a.timeNow().Before(a.facts.TerminalApprovedUntil()) {
		return nil, errRefused{"the node terminal, which the machine's owner has not approved"}
	}
	if _, err := a.cs.CoreV1().Nodes().Get(ctx, node, metav1.GetOptions{}); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil, errRefused{"a node terminal on " + node + ", which is not in this cluster"}
		}
		return nil, err
	}
	return appk8s.CreateShellPod(ctx, a.cs, node, name)
}

// closeShell removes a node terminal's pod. One already gone is closed.
func (a *Agent) closeShell(ctx context.Context, name string) (string, error) {
	if !shellName.MatchString(name) {
		return "", errRefused{"closing a pod named " + name}
	}
	if _, err := a.cs.CoreV1().Pods("kube-system").Get(ctx, name, metav1.GetOptions{}); k8serrors.IsNotFound(err) {
		return "the node terminal " + name + ", already closed", nil
	}
	if !a.facts.Shell(name) {
		return "", errRefused{"pod " + name + " is not a node terminal"}
	}
	appk8s.DeleteShellPod(ctx, a.cs, name)
	return "closing the node terminal " + name, nil
}

// RootlessBuilds makes every build Job the agent creates rootless: uid 1000,
// unprivileged, no added capabilities, with seccomp and AppArmor left off
// because the builder makes its own user namespace inside the pod. That
// namespace is mapped by newuidmap and newgidmap, which hold file
// capabilities: so privilege escalation stays allowed and the default
// capabilities stay, or the kernel ignores them and the build cannot start.
//
// On a host enforcing SELinux the pod runs as container_engine_t, the type
// container-selinux gives a container engine inside a container: the default
// container_t may not mount the devpts a build step needs. It stays confined,
// unlike the privileged builder's spc_t; hosts without SELinux ignore it.
//
// Rootless, the builder builds everything with BuildKit, so the build cache
// keeps only BuildKit's folder, and a folder of its own: a cache a builder
// running as root wrote holds files uid 1000 can neither use nor remove.
type RootlessBuilds struct{}

// BuilderSELinuxType is the SELinux type rootless builds run as.
const BuilderSELinuxType = "container_engine_t"

// RootlessCacheSubPath is the build cache's folder for rootless builds.
const RootlessCacheSubPath = "buildkit-rootless"

func (RootlessBuilds) MutateJob(_ context.Context, kind appk8s.JobKind, job *batchv1.Job) error {
	if kind != appk8s.JobBuild {
		return nil
	}
	uid := int64(1000)
	no := false
	spec := &job.Spec.Template.Spec
	if spec.SecurityContext == nil {
		spec.SecurityContext = &corev1.PodSecurityContext{}
	}
	spec.SecurityContext.FSGroup = &uid
	// The cache is handed to uid 1000's group once, not walked on every build.
	onMismatch := corev1.FSGroupChangeOnRootMismatch
	spec.SecurityContext.FSGroupChangePolicy = &onMismatch
	spec.SecurityContext.RunAsUser = &uid
	spec.SecurityContext.RunAsGroup = &uid
	for _, cs := range [][]corev1.Container{spec.InitContainers, spec.Containers} {
		for i := range cs {
			cs[i].SecurityContext = &corev1.SecurityContext{
				RunAsUser:       &uid,
				RunAsGroup:      &uid,
				RunAsNonRoot:    boolPtr(true),
				Privileged:      &no,
				SELinuxOptions:  &corev1.SELinuxOptions{Type: BuilderSELinuxType},
				SeccompProfile:  &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeUnconfined},
				AppArmorProfile: &corev1.AppArmorProfile{Type: corev1.AppArmorProfileTypeUnconfined},
			}
			cs[i].VolumeMounts = rootlessCache(cs[i].VolumeMounts)
		}
	}
	return nil
}

// rootlessCache keeps BuildKit's mount of the build cache, moved to the
// rootless folder, and drops Buildah's.
func rootlessCache(mounts []corev1.VolumeMount) []corev1.VolumeMount {
	var kept []corev1.VolumeMount
	for _, m := range mounts {
		if m.Name == appk8s.BuildCacheVolume {
			if m.SubPath != appk8s.BuildKitCacheSubPath {
				continue
			}
			m.SubPath = RootlessCacheSubPath
		}
		kept = append(kept, m)
	}
	return kept
}

func boolPtr(b bool) *bool { return &b }

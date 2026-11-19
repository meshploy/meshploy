package k8s

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"
)

// The builder runs only while a build does, so between builds no container
// uses it, and the kubelet removes an image no container has used for a day
// (imageMaximumGCAge), one never run within minutes of its download, or any of
// them when its disk fills. The next build then downloads all of it first,
// which on a slow link is most of the build. KeepBuilder downloads it ahead on
// a build node and pins it: the kubelet's garbage collection and crictl's prune
// (Free space) both leave a pinned image. Earlier images of the builder's
// repository are unpinned, so they go as any other.

// keepBuilderApp labels the pods that keep the builder.
const keepBuilderApp = "meshploy-builder-keep"

// keepBuilderDeadline bounds one download: half a gigabyte at 40 KB/s takes
// about three hours.
const keepBuilderDeadline = 4 * 60 * 60

// keepBuilderScript pulls $IMG, pins every name the pulled image has in $REPO
// and unpins the rest of $REPO. Both come from the pod's environment, checked
// by validBuilderRef first.
const keepBuilderScript = `PATH=/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin
export PATH
imgid() { k3s crictl inspecti -o go-template --template '{{.status.id}}' "$1" 2>/dev/null; }
k3s crictl pull "$IMG" >/dev/null || exit $?
cur=$(imgid "$IMG")
[ -n "$cur" ] || { echo "the builder is not on this node after pulling it"; exit 1; }
for ref in $(k3s ctr -n k8s.io images ls -q); do
  case "$ref" in "$REPO:"*|"$REPO@"*) ;; *) continue ;; esac
  if [ "$(imgid "$ref")" = "$cur" ]; then
    k3s ctr -n k8s.io images label "$ref" io.cri-containerd.pinned=pinned >/dev/null || exit $?
  else
    k3s ctr -n k8s.io images label "$ref" io.cri-containerd.pinned= >/dev/null || exit $?
  fi
done
echo "kept $IMG"`

// builderRef is an image reference that names its registry: containerd lists
// images under the full name, which the script matches against.
var builderRef = regexp.MustCompile(`^([a-z0-9]([a-z0-9.-]*[a-z0-9])?(:[0-9]{1,5})?)((/[a-z0-9]+([._-][a-z0-9]+)*)+)(:[A-Za-z0-9_][A-Za-z0-9._-]{0,127})?(@sha256:[a-f0-9]{64})?$`)

// builderRepo is the repository of a builder reference, without its tag or
// digest; "" for a reference the keeper does not take.
func builderRepo(image string) string {
	m := builderRef.FindStringSubmatch(image)
	if m == nil {
		return ""
	}
	host := m[1]
	// A registry is a host with a dot or a port, or localhost; without one
	// the name is Docker Hub's, which containerd lists in full.
	if !strings.ContainsAny(host, ".:") && host != "localhost" {
		return ""
	}
	if m[7] == "" && m[8] == "" {
		return ""
	}
	return host + m[4]
}

// KeepBuilder starts keeping image on node: downloading it, unless the node
// has it, and pinning it. It does not wait for the download, which on a slow
// link takes hours; started is false when one is already running there.
// Finished runs on the node are removed first.
func KeepBuilder(ctx context.Context, client kubernetes.Interface, node, image string) (started bool, err error) {
	repo := builderRepo(image)
	if repo == "" {
		return false, fmt.Errorf("keep the builder on %s: %q is not an image reference with a registry and a tag or digest", node, image)
	}
	pods := client.CoreV1().Pods("kube-system")
	list, err := pods.List(ctx, metav1.ListOptions{
		LabelSelector: "app=" + keepBuilderApp,
		FieldSelector: fields.OneTermEqualSelector("spec.nodeName", node).String(),
	})
	if err != nil {
		return false, fmt.Errorf("keep the builder on %s: %w", node, err)
	}
	for _, p := range list.Items {
		if p.Spec.NodeName != node {
			continue
		}
		switch p.Status.Phase {
		case corev1.PodSucceeded, corev1.PodFailed:
			_ = pods.Delete(ctx, p.Name, metav1.DeleteOptions{})
		default:
			if p.DeletionTimestamp == nil {
				return false, nil
			}
		}
	}
	pod := hostPod(node, keepBuilderApp+"-", keepBuilderApp, "keep", keepBuilderScript,
		[]corev1.EnvVar{{Name: "IMG", Value: image}, {Name: "REPO", Value: repo}})
	deadline := int64(keepBuilderDeadline)
	pod.Spec.ActiveDeadlineSeconds = &deadline
	if _, err := pods.Create(ctx, pod, metav1.CreateOptions{}); err != nil {
		return false, fmt.Errorf("keep the builder on %s: %w", node, err)
	}
	return true, nil
}

// BuildNodes are the cluster's nodes that take builds and are ready.
func BuildNodes(ctx context.Context, client kubernetes.Interface) ([]string, error) {
	list, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{LabelSelector: BuilderNodeLabel + "=" + BuilderNodeValue})
	if err != nil {
		return nil, err
	}
	var out []string
	for _, n := range list.Items {
		for _, c := range n.Status.Conditions {
			if c.Type == corev1.NodeReady && c.Status == corev1.ConditionTrue {
				out = append(out, n.Name)
			}
		}
	}
	return out, nil
}

package k8s

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// A build's memory is always capped, at 4Gi or the request when that is
// larger; its CPU only when asked. A value that does not parse falls back
// instead of panicking.
func TestBuilderResources(t *testing.T) {
	cases := []struct {
		name           string
		p              BuildJobParams
		memLim, cpuLim string
	}{
		{"defaults cap memory, not CPU", BuildJobParams{}, "4Gi", ""},
		{"a larger request raises the cap", BuildJobParams{MemoryRequest: "8Gi"}, "8Gi", ""},
		{"explicit limits", BuildJobParams{CPULimit: "2", MemoryLimit: "6Gi"}, "6Gi", "2"},
		{"unparsable values fall back", BuildJobParams{MemoryRequest: "1 GB", MemoryLimit: "lots", CPULimit: "many"}, "4Gi", ""},
	}
	for _, c := range cases {
		r := BuilderResources(c.p)
		if got := r.Limits.Memory().String(); got != c.memLim {
			t.Errorf("%s: memory limit %s, want %s", c.name, got, c.memLim)
		}
		cpu := ""
		if q, ok := r.Limits[corev1.ResourceCPU]; ok {
			cpu = q.String()
		}
		if cpu != c.cpuLim {
			t.Errorf("%s: CPU limit %q, want %q", c.name, cpu, c.cpuLim)
		}
	}

	client := fake.NewSimpleClientset()
	if err := CreateBuildJob(t.Context(), client, BuildJobParams{JobName: "build-1", Namespace: "demo"}); err != nil {
		t.Fatal(err)
	}
	job, err := client.BatchV1().Jobs("demo").Get(t.Context(), "build-1", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := job.Spec.Template.Spec.Containers[0].Resources.Limits.Memory().String(); got != "4Gi" {
		t.Errorf("the build job's memory limit is %s, want 4Gi", got)
	}
}

func TestOOMKilled(t *testing.T) {
	killed := func(reason string, last bool) *corev1.Pod {
		term := &corev1.ContainerStateTerminated{Reason: reason}
		st := corev1.ContainerStatus{Name: "builder"}
		if last {
			st.LastTerminationState.Terminated = term
		} else {
			st.State.Terminated = term
		}
		return &corev1.Pod{Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{st}}}
	}
	if !oomKilled(killed("OOMKilled", false)) || !oomKilled(killed("OOMKilled", true)) {
		t.Error("an OOM kill was not recognised")
	}
	if oomKilled(killed("Error", false)) || oomKilled(nil) {
		t.Error("a failure that was not an OOM kill was reported as one")
	}
}

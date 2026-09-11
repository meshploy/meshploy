package k8s

import (
	"context"
	"reflect"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// A command reaches the container as written: Kubernetes would expand $(VAR)
// and turn $$ into $, so every $ is doubled for it. No command keeps the
// image's own.
func TestContainerCommandIsPassedAsWritten(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset()

	if err := ApplyDeployment(ctx, client, WorkloadParams{
		Name: "worker", Namespace: "ns", Image: "alpine",
		Command: []string{"/bin/sh", "-c"},
		Args:    []string{"echo $HOME $(NAME) $$"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := ApplyDeployment(ctx, client, WorkloadParams{Name: "plain", Namespace: "ns", Image: "nginx"}); err != nil {
		t.Fatal(err)
	}

	get := func(name string) ([]string, []string) {
		dep, err := client.AppsV1().Deployments("ns").Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		c := dep.Spec.Template.Spec.Containers[0]
		return c.Command, c.Args
	}
	cmd, args := get("worker")
	if !reflect.DeepEqual(cmd, []string{"/bin/sh", "-c"}) || !reflect.DeepEqual(args, []string{"echo $$HOME $$(NAME) $$$$"}) {
		t.Errorf("worker: command %q, args %q", cmd, args)
	}
	if cmd, args := get("plain"); cmd != nil || args != nil {
		t.Errorf("plain: command %q, args %q; want the image's own", cmd, args)
	}
}

package k8s

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

// Services deployed together into a new project each ensure its namespace at
// once: none fails, it is created once, and its steps run before any returns.
func TestEnsureNamespaceConcurrent(t *testing.T) {
	client := fake.NewSimpleClientset()
	var creates, steps atomic.Int32
	client.PrependReactor("create", "namespaces", func(k8stesting.Action) (bool, runtime.Object, error) {
		creates.Add(1)
		return false, nil, nil
	})
	OnNamespaceCreatedIn(client, func(context.Context, string) error { steps.Add(1); return nil })

	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := EnsureNamespace(context.Background(), client, "race-apps"); err != nil {
				errs <- err
				return
			}
			if steps.Load() == 0 {
				errs <- errNoSteps
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	if n := creates.Load(); n != 1 {
		t.Fatalf("namespace created %d times, want 1", n)
	}
}

// A namespace another process made between the look and the create is taken
// as made, and its steps still run.
func TestEnsureNamespaceMadeElsewhere(t *testing.T) {
	client := fake.NewSimpleClientset()
	first := true
	client.PrependReactor("get", "namespaces", func(a k8stesting.Action) (bool, runtime.Object, error) {
		if first {
			first = false
			// Someone else creates it right after this look says it is missing.
			_ = client.Tracker().Add(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "elsewhere"}})
			return true, nil, notFound("elsewhere")
		}
		return false, nil, nil
	})
	var ran bool
	OnNamespaceCreatedIn(client, func(context.Context, string) error { ran = true; return nil })
	if err := EnsureNamespace(context.Background(), client, "elsewhere"); err != nil {
		t.Fatalf("EnsureNamespace: %v", err)
	}
	if !ran {
		t.Fatal("the new-namespace steps did not run")
	}
}

type testErr string

func (e testErr) Error() string { return string(e) }

const errNoSteps = testErr("returned before the namespace's steps ran")

func notFound(name string) error {
	return k8serrors.NewNotFound(schema.GroupResource{Resource: "namespaces"}, name)
}

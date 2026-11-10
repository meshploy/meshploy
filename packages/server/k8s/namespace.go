package k8s

import (
	"context"
	"sync"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// namespaceCreated are called once a namespace has been made, before
// anything is deployed into it: those of every cluster, and those of one
// cluster, keyed by its client.
var (
	namespaceMu        sync.RWMutex
	namespaceCreated   []func(ctx context.Context, name string) error
	namespaceCreatedIn = map[kubernetes.Interface][]func(ctx context.Context, name string) error{}
)

// OnNamespaceCreated adds a step every new namespace takes before anything
// runs in it: its isolation, so a new project is never open in between.
// Call it before the server starts. An error fails the deploy that made the
// namespace.
func OnNamespaceCreated(fn func(ctx context.Context, name string) error) {
	namespaceMu.Lock()
	defer namespaceMu.Unlock()
	namespaceCreated = append(namespaceCreated, fn)
}

// OnNamespaceCreatedIn is OnNamespaceCreated for the namespaces made through
// client alone: a step that acts on one cluster never runs for another's.
func OnNamespaceCreatedIn(client kubernetes.Interface, fn func(ctx context.Context, name string) error) {
	namespaceMu.Lock()
	defer namespaceMu.Unlock()
	namespaceCreatedIn[client] = append(namespaceCreatedIn[client], fn)
}

// EnsureNamespace creates the namespace if it does not already exist.
func EnsureNamespace(ctx context.Context, client kubernetes.Interface, name string) error {
	_, err := client.CoreV1().Namespaces().Get(ctx, name, metav1.GetOptions{})
	if err == nil {
		return nil
	}
	if !k8serrors.IsNotFound(err) {
		return err
	}
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: map[string]string{"managed-by": "meshploy"},
		},
	}
	if _, err = client.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{}); err != nil {
		return err
	}
	namespaceMu.RLock()
	steps := append(append([]func(context.Context, string) error{}, namespaceCreated...), namespaceCreatedIn[client]...)
	namespaceMu.RUnlock()
	for _, fn := range steps {
		if err := fn(ctx, name); err != nil {
			return err
		}
	}
	return nil
}

// DeleteNamespace removes the namespace and everything in it. A namespace
// that is already gone is not an error.
func DeleteNamespace(ctx context.Context, client kubernetes.Interface, name string) error {
	err := client.CoreV1().Namespaces().Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil && !k8serrors.IsNotFound(err) {
		return err
	}
	return nil
}

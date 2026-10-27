package k8s

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// namespaceCreated are called once a namespace has been made, before
// anything is deployed into it.
var namespaceCreated []func(ctx context.Context, name string) error

// OnNamespaceCreated adds a step every new namespace takes before anything
// runs in it: its isolation, so a new project is never open in between.
// Call it before the server starts. An error fails the deploy that made the
// namespace.
func OnNamespaceCreated(fn func(ctx context.Context, name string) error) {
	namespaceCreated = append(namespaceCreated, fn)
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
	for _, fn := range namespaceCreated {
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

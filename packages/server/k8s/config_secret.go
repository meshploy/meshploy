package k8s

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// configSecretName is the Secret holding a workload's config files.
//
// One Secret per workload, not one per config file. A file shared by several
// services could be a single shared Secret, but that object would belong to no
// one: nothing deletes it when the last service detaches, and it survives with
// no record pointing at it — the same way a Deployment outlived its service and
// ran unnoticed for months. A per-workload Secret is owned by one workload and
// dies with it.
func configSecretName(workload string) string { return workload + "-config" }

// configSecretKey names one file's entry. Keys are positional because a Secret
// key cannot contain "/" and two files may share a basename.
func configSecretKey(i int) string { return fmt.Sprintf("file-%d", i) }

// ApplyConfigSecret writes the workload's config files into its Secret, or
// removes the Secret when there are none left.
func ApplyConfigSecret(ctx context.Context, client kubernetes.Interface, workload, namespace string, files []ConfigFileMount) error {
	if client == nil {
		return nil
	}
	name := configSecretName(workload)
	api := client.CoreV1().Secrets(namespace)

	if len(files) == 0 {
		// Detaching the last file should take the Secret with it rather than
		// leaving an empty object behind.
		if err := api.Delete(ctx, name, metav1.DeleteOptions{}); err != nil && !k8serrors.IsNotFound(err) {
			return fmt.Errorf("delete config secret %s: %w", name, err)
		}
		return nil
	}

	data := make(map[string][]byte, len(files))
	for i, f := range files {
		data[configSecretKey(i)] = []byte(f.Content)
	}
	desired := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    map[string]string{"app": workload, "managed-by": "meshploy"},
		},
		Type: corev1.SecretTypeOpaque,
		Data: data,
	}

	existing, err := api.Get(ctx, name, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		_, err = api.Create(ctx, desired, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}
	desired.ResourceVersion = existing.ResourceVersion
	_, err = api.Update(ctx, desired, metav1.UpdateOptions{})
	return err
}

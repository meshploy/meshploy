package k8s

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
)

// WorkloadParams describes a service to deploy.
type WorkloadParams struct {
	Name      string // K8s resource name (slug)
	Namespace string // project slug
	Image     string
	Port      int32
	Replicas  int32
	Env       []corev1.EnvVar

	CPURequest    string // "100m"
	CPULimit      string // "500m"
	MemoryRequest string // "128Mi"
	MemoryLimit   string // "512Mi"

	// NodeName pins the pod to a specific node (optional).
	NodeName string
}

// ApplyDeployment creates or updates a K8s Deployment.
func ApplyDeployment(ctx context.Context, client kubernetes.Interface, p WorkloadParams) error {
	replicas := p.Replicas
	if replicas == 0 {
		replicas = 1
	}

	labels := map[string]string{
		"app":        p.Name,
		"managed-by": "meshploy",
	}

	resources := corev1.ResourceRequirements{}
	if p.CPURequest != "" || p.MemoryRequest != "" {
		resources.Requests = corev1.ResourceList{}
		if p.CPURequest != "" {
			resources.Requests[corev1.ResourceCPU] = resource.MustParse(p.CPURequest)
		}
		if p.MemoryRequest != "" {
			resources.Requests[corev1.ResourceMemory] = resource.MustParse(p.MemoryRequest)
		}
	}
	if p.CPULimit != "" || p.MemoryLimit != "" {
		resources.Limits = corev1.ResourceList{}
		if p.CPULimit != "" {
			resources.Limits[corev1.ResourceCPU] = resource.MustParse(p.CPULimit)
		}
		if p.MemoryLimit != "" {
			resources.Limits[corev1.ResourceMemory] = resource.MustParse(p.MemoryLimit)
		}
	}

	podSpec := corev1.PodSpec{
		Containers: []corev1.Container{
			{
				Name:            p.Name,
				Image:           p.Image,
				ImagePullPolicy: corev1.PullAlways,
				Ports: []corev1.ContainerPort{
					{ContainerPort: p.Port, Protocol: corev1.ProtocolTCP},
				},
				Env:       p.Env,
				Resources: resources,
			},
		},
	}
	if p.NodeName != "" {
		podSpec.NodeName = p.NodeName
	}

	desired := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      p.Name,
			Namespace: p.Namespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec:       podSpec,
			},
		},
	}

	_, err := client.AppsV1().Deployments(p.Namespace).Get(ctx, p.Name, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		_, err = client.AppsV1().Deployments(p.Namespace).Create(ctx, desired, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}
	_, err = client.AppsV1().Deployments(p.Namespace).Update(ctx, desired, metav1.UpdateOptions{})
	return err
}

// ApplyService creates or updates a ClusterIP Service for the workload.
func ApplyService(ctx context.Context, client kubernetes.Interface, name, namespace string, port int32) error {
	labels := map[string]string{"app": name, "managed-by": "meshploy"}

	desired := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": name},
			Ports: []corev1.ServicePort{
				{
					Port:       port,
					TargetPort: intstr.FromInt32(port),
					Protocol:   corev1.ProtocolTCP,
				},
			},
			Type: corev1.ServiceTypeClusterIP,
		},
	}

	_, err := client.CoreV1().Services(namespace).Get(ctx, name, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		_, err = client.CoreV1().Services(namespace).Create(ctx, desired, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}
	_, err = client.CoreV1().Services(namespace).Update(ctx, desired, metav1.UpdateOptions{})
	return err
}

// DeleteWorkload removes the Deployment and Service for a service.
func DeleteWorkload(ctx context.Context, client kubernetes.Interface, name, namespace string) error {
	dp := client.AppsV1().Deployments(namespace)
	if err := dp.Delete(ctx, name, metav1.DeleteOptions{}); err != nil && !k8serrors.IsNotFound(err) {
		return fmt.Errorf("delete deployment: %w", err)
	}
	svc := client.CoreV1().Services(namespace)
	if err := svc.Delete(ctx, name, metav1.DeleteOptions{}); err != nil && !k8serrors.IsNotFound(err) {
		return fmt.Errorf("delete service: %w", err)
	}
	return nil
}

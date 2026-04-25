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
					{ContainerPort: p.Port, HostPort: p.Port, Protocol: corev1.ProtocolTCP},
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

// DatabaseWorkloadParams describes a managed-database workload.
type DatabaseWorkloadParams struct {
	Name      string // K8s resource name (slug)
	Namespace string
	Image     string
	Port      int32
	Env       []corev1.EnvVar
	StorageGB int    // PVC size in GiB
	DataPath  string // mount path inside container
	NodeName  string // "" = auto-schedule
}

// ApplyDatabaseDeployment creates-or-updates a PVC and Deployment for a managed database.
func ApplyDatabaseDeployment(ctx context.Context, client kubernetes.Interface, p DatabaseWorkloadParams) error {
	pvcName := p.Name + "-data"
	storageGB := p.StorageGB
	if storageGB == 0 {
		storageGB = 10
	}

	// Ensure the data PVC exists.
	_, err := client.CoreV1().PersistentVolumeClaims(p.Namespace).Get(ctx, pvcName, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		qty := resource.MustParse(fmt.Sprintf("%dGi", storageGB))
		pvc := &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      pvcName,
				Namespace: p.Namespace,
				Labels:    map[string]string{"app": p.Name, "managed-by": "meshploy"},
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{corev1.ResourceStorage: qty},
				},
			},
		}
		if _, err = client.CoreV1().PersistentVolumeClaims(p.Namespace).Create(ctx, pvc, metav1.CreateOptions{}); err != nil {
			return fmt.Errorf("create database PVC: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("check database PVC: %w", err)
	}

	labels := map[string]string{"app": p.Name, "managed-by": "meshploy"}
	replicas := int32(1)
	podSpec := corev1.PodSpec{
		Containers: []corev1.Container{
			{
				Name:            p.Name,
				Image:           p.Image,
				ImagePullPolicy: corev1.PullIfNotPresent,
				Ports:           []corev1.ContainerPort{{ContainerPort: p.Port, Protocol: corev1.ProtocolTCP}},
				Env:             p.Env,
				VolumeMounts: []corev1.VolumeMount{
					{Name: "data", MountPath: p.DataPath},
				},
			},
		},
		Volumes: []corev1.Volume{
			{
				Name: "data",
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: pvcName},
				},
			},
		},
	}
	if p.NodeName != "" {
		podSpec.NodeName = p.NodeName
	}

	desired := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: p.Name, Namespace: p.Namespace, Labels: labels},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec:       podSpec,
			},
		},
	}

	_, err = client.AppsV1().Deployments(p.Namespace).Get(ctx, p.Name, metav1.GetOptions{})
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

// DeleteDatabasePVC removes the data PVC for a database workload.
// The PVC is recreated on the next provision (used for reset/wipe).
func DeleteDatabasePVC(ctx context.Context, client kubernetes.Interface, name, namespace string) error {
	err := client.CoreV1().PersistentVolumeClaims(namespace).Delete(ctx, name+"-data", metav1.DeleteOptions{})
	if err != nil && !k8serrors.IsNotFound(err) {
		return fmt.Errorf("delete database PVC: %w", err)
	}
	return nil
}

// ApplyNodePortService creates-or-updates a NodePort Service for mesh-external access.
// Returns the assigned NodePort so the caller can store it.
func ApplyNodePortService(ctx context.Context, client kubernetes.Interface, name, namespace string, port int32) (int32, error) {
	npName := name + "-nodeport"
	labels := map[string]string{"app": name, "managed-by": "meshploy"}

	desired := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      npName,
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
			Type: corev1.ServiceTypeNodePort,
		},
	}

	existing, err := client.CoreV1().Services(namespace).Get(ctx, npName, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		created, err := client.CoreV1().Services(namespace).Create(ctx, desired, metav1.CreateOptions{})
		if err != nil {
			return 0, err
		}
		if len(created.Spec.Ports) > 0 {
			return created.Spec.Ports[0].NodePort, nil
		}
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	// Preserve the existing NodePort on update so it doesn't change.
	if len(existing.Spec.Ports) > 0 {
		desired.Spec.Ports[0].NodePort = existing.Spec.Ports[0].NodePort
	}
	updated, err := client.CoreV1().Services(namespace).Update(ctx, desired, metav1.UpdateOptions{})
	if err != nil {
		return 0, err
	}
	if len(updated.Spec.Ports) > 0 {
		return updated.Spec.Ports[0].NodePort, nil
	}
	return 0, nil
}

// ScaleDeployment sets the replica count on an existing Deployment.
// If the Deployment does not exist (not yet deployed) the call is a no-op.
func ScaleDeployment(ctx context.Context, client kubernetes.Interface, name, namespace string, replicas int32) error {
	scale, err := client.AppsV1().Deployments(namespace).GetScale(ctx, name, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("get scale: %w", err)
	}
	scale.Spec.Replicas = replicas
	_, err = client.AppsV1().Deployments(namespace).UpdateScale(ctx, name, scale, metav1.UpdateOptions{})
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

package k8s

import (
	"context"
	"encoding/base64"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
)

// PortSpec describes one port to expose on a K8s Service.
type PortSpec struct {
	Name     string // K8s port name (e.g. "http", "grpc")
	Port     int32  // container port
	IsPublic bool   // include in the NodePort service
	NodePort int32  // existing NodePort to preserve on update; 0 = let K8s assign
}

// WorkloadParams describes a service to deploy.
type WorkloadParams struct {
	Name      string // K8s resource name (slug)
	Namespace string // project slug
	Image     string
	Ports     []PortSpec
	Replicas  int32
	Env       []corev1.EnvVar

	CPURequest    string // "100m"
	CPULimit      string // "500m"
	MemoryRequest string // "128Mi"
	MemoryLimit   string // "512Mi"

	// NodeName pins the pod to a specific node (optional).
	NodeName string

	// Optional K8s liveness and readiness probes (both use the same spec when set).
	LivenessProbe  *corev1.Probe
	ReadinessProbe *corev1.Probe

	// User-managed volume mounts (backed by standalone PVCs).
	VolumeMounts []VolumeAttachment

	// ImagePullSecretName is the name of a docker-registry Secret in the same
	// namespace to use as an imagePullSecret. Empty = public image (no auth needed).
	ImagePullSecretName string
}

// VolumeAttachment wires a standalone PVC into a container at a given path.
type VolumeAttachment struct {
	PVCName   string // K8s PVC name
	MountPath string
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

	cPorts := make([]corev1.ContainerPort, len(p.Ports))
	for i, sp := range p.Ports {
		cPorts[i] = corev1.ContainerPort{
			Name:          sp.Name,
			ContainerPort: sp.Port,
			Protocol:      corev1.ProtocolTCP,
		}
	}
	container := corev1.Container{
		Name:            p.Name,
		Image:           p.Image,
		ImagePullPolicy: corev1.PullAlways,
		Ports:           cPorts,
		Env:            p.Env,
		Resources:      resources,
		LivenessProbe:  p.LivenessProbe,
		ReadinessProbe: p.ReadinessProbe,
	}
	var podVolumes []corev1.Volume
	for i, va := range p.VolumeMounts {
		volName := fmt.Sprintf("vol-%d", i)
		container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{
			Name:      volName,
			MountPath: va.MountPath,
		})
		podVolumes = append(podVolumes, corev1.Volume{
			Name: volName,
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: va.PVCName},
			},
		})
	}
	podSpec := corev1.PodSpec{
		Containers: []corev1.Container{container},
		Volumes:    podVolumes,
	}
	if p.NodeName != "" {
		podSpec.NodeName = p.NodeName
	}
	if p.ImagePullSecretName != "" {
		podSpec.ImagePullSecrets = []corev1.LocalObjectReference{{Name: p.ImagePullSecretName}}
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
// All ports are included so any pod in the cluster can reach them via DNS.
func ApplyService(ctx context.Context, client kubernetes.Interface, name, namespace string, ports []PortSpec) error {
	labels := map[string]string{"app": name, "managed-by": "meshploy"}

	svcPorts := make([]corev1.ServicePort, len(ports))
	for i, p := range ports {
		svcPorts[i] = corev1.ServicePort{
			Name:       p.Name,
			Port:       p.Port,
			TargetPort: intstr.FromInt32(p.Port),
			Protocol:   corev1.ProtocolTCP,
		}
	}

	desired := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": name},
			Ports:    svcPorts,
			Type:     corev1.ServiceTypeClusterIP,
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

	LivenessProbe  *corev1.Probe
	ReadinessProbe *corev1.Probe
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
	dbContainer := corev1.Container{
		Name:            p.Name,
		Image:           p.Image,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Ports:           []corev1.ContainerPort{{ContainerPort: p.Port, Protocol: corev1.ProtocolTCP}},
		Env:             p.Env,
		VolumeMounts: []corev1.VolumeMount{
			{Name: "data", MountPath: p.DataPath},
		},
		LivenessProbe:  p.LivenessProbe,
		ReadinessProbe: p.ReadinessProbe,
	}
	podSpec := corev1.PodSpec{
		Containers: []corev1.Container{dbContainer},
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

// ApplyNodePortService creates-or-updates a NodePort Service for public ports only.
// Returns a map of port name → assigned NodePort so callers can persist them.
func ApplyNodePortService(ctx context.Context, client kubernetes.Interface, name, namespace string, ports []PortSpec) (map[string]int32, error) {
	npName := name + "-nodeport"
	labels := map[string]string{"app": name, "managed-by": "meshploy"}

	var publicPorts []PortSpec
	for _, p := range ports {
		if p.IsPublic {
			publicPorts = append(publicPorts, p)
		}
	}

	// No public ports — delete the NodePort service if it exists.
	if len(publicPorts) == 0 {
		err := client.CoreV1().Services(namespace).Delete(ctx, npName, metav1.DeleteOptions{})
		if err != nil && !k8serrors.IsNotFound(err) {
			return nil, fmt.Errorf("delete nodeport service: %w", err)
		}
		return map[string]int32{}, nil
	}

	svcPorts := make([]corev1.ServicePort, len(publicPorts))
	for i, p := range publicPorts {
		svcPorts[i] = corev1.ServicePort{
			Name:       p.Name,
			Port:       p.Port,
			TargetPort: intstr.FromInt32(p.Port),
			Protocol:   corev1.ProtocolTCP,
			NodePort:   p.NodePort, // 0 = let K8s assign; non-zero = preserve
		}
	}

	desired := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      npName,
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": name},
			Ports:    svcPorts,
			Type:     corev1.ServiceTypeNodePort,
		},
	}

	existing, err := client.CoreV1().Services(namespace).Get(ctx, npName, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		created, err := client.CoreV1().Services(namespace).Create(ctx, desired, metav1.CreateOptions{})
		if err != nil {
			return nil, err
		}
		return extractNodePorts(created.Spec.Ports), nil
	}
	if err != nil {
		return nil, err
	}

	// Preserve existing NodePorts by name so they don't change on update.
	existingNPs := extractNodePorts(existing.Spec.Ports)
	for i, sp := range desired.Spec.Ports {
		if np, ok := existingNPs[sp.Name]; ok {
			desired.Spec.Ports[i].NodePort = np
		}
	}

	updated, err := client.CoreV1().Services(namespace).Update(ctx, desired, metav1.UpdateOptions{})
	if err != nil {
		return nil, err
	}
	return extractNodePorts(updated.Spec.Ports), nil
}

func extractNodePorts(ports []corev1.ServicePort) map[string]int32 {
	m := make(map[string]int32, len(ports))
	for _, p := range ports {
		m[p.Name] = p.NodePort
	}
	return m
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

// PodInfo is a summarised view of a running pod for the UI.
type PodInfo struct {
	Name      string `json:"name"`
	Phase     string `json:"phase"`
	Ready     bool   `json:"ready"`
	Restarts  int32  `json:"restarts"`
	NodeName  string `json:"node_name"`
	StartedAt string `json:"started_at"` // RFC3339, empty if not yet started
}

// ListServicePods returns all pods for a given app label in the namespace,
// sorted by creation timestamp (newest first).
func ListServicePods(ctx context.Context, client kubernetes.Interface, namespace, appLabel string) ([]PodInfo, error) {
	list, err := client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("app=%s,managed-by=meshploy", appLabel),
	})
	if err != nil {
		return nil, err
	}
	pods := make([]PodInfo, 0, len(list.Items))
	for _, p := range list.Items {
		var restarts int32
		var ready bool
		for _, cs := range p.Status.ContainerStatuses {
			restarts += cs.RestartCount
			if cs.Ready {
				ready = true
			}
		}
		startedAt := ""
		if p.Status.StartTime != nil {
			startedAt = p.Status.StartTime.UTC().Format("2006-01-02T15:04:05Z")
		}
		pods = append(pods, PodInfo{
			Name:      p.Name,
			Phase:     string(p.Status.Phase),
			Ready:     ready,
			Restarts:  restarts,
			NodeName:  p.Spec.NodeName,
			StartedAt: startedAt,
		})
	}
	return pods, nil
}

// EnsureRegistryPullSecret creates or updates a docker-registry Secret in the
// given namespace. The secret is named `name` and holds credentials for `server`.
// Call this before ApplyDeployment when the image is in a private registry.
func EnsureRegistryPullSecret(ctx context.Context, client kubernetes.Interface, namespace, name, server, username, password string) error {
	auth := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
	dockerConfig := fmt.Sprintf(`{"auths":{%q:{"auth":%q}}}`, server, auth)
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    map[string]string{"managed-by": "meshploy"},
		},
		Type: corev1.SecretTypeDockerConfigJson,
		Data: map[string][]byte{
			corev1.DockerConfigJsonKey: []byte(dockerConfig),
		},
	}
	_, err := client.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		_, err = client.CoreV1().Secrets(namespace).Create(ctx, secret, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}
	_, err = client.CoreV1().Secrets(namespace).Update(ctx, secret, metav1.UpdateOptions{})
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

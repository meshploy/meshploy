package k8s

import (
	"fmt"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// NewClient returns a Kubernetes clientset.
// If kubeconfigPath is set it loads that file; otherwise it falls back to
// in-cluster config (works when the API pod runs inside K3s).
// An optional serverURL overrides the server address in the kubeconfig — useful
// when the API runs in Docker and the kubeconfig points to 127.0.0.1.
func NewClient(kubeconfigPath string, serverURL ...string) (*kubernetes.Clientset, error) {
	var cfg *rest.Config
	var err error

	if kubeconfigPath != "" {
		cfg, err = clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	} else {
		cfg, err = rest.InClusterConfig()
	}
	if err != nil {
		return nil, fmt.Errorf("k8s config: %w", err)
	}
	if len(serverURL) > 0 && serverURL[0] != "" {
		cfg.Host = serverURL[0]
	}
	return kubernetes.NewForConfig(cfg)
}

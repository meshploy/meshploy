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
	cs, _, err := NewClientWithConfig(kubeconfigPath, serverURL...)
	return cs, err
}

// NewClientWithConfig is like NewClient but also returns the REST config,
// needed for operations like port-forwarding that require SPDY transport.
func NewClientWithConfig(kubeconfigPath string, serverURL ...string) (*kubernetes.Clientset, *rest.Config, error) {
	var cfg *rest.Config
	var err error

	if kubeconfigPath != "" {
		cfg, err = clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	} else {
		cfg, err = rest.InClusterConfig()
	}
	if err != nil {
		return nil, nil, fmt.Errorf("k8s config: %w", err)
	}
	if len(serverURL) > 0 && serverURL[0] != "" {
		cfg.Host = serverURL[0]
		cfg.TLSClientConfig.Insecure = true
		cfg.TLSClientConfig.CAData = nil
		cfg.TLSClientConfig.CAFile = ""
	}
	cs, err := kubernetes.NewForConfig(cfg)
	return cs, cfg, err
}

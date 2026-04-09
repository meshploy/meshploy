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
		// The k3s TLS cert is only valid for 127.0.0.1. When routing via
		// host.meshploy.internal the hostname won't match, so skip server cert
		// verification. This is safe: the connection stays on-host.
		cfg.TLSClientConfig.Insecure = true
		cfg.TLSClientConfig.CAData = nil
		cfg.TLSClientConfig.CAFile = ""
	}
	return kubernetes.NewForConfig(cfg)
}

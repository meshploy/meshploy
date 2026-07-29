package k8s

import (
	"fmt"
	"log"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// DefaultTLSServerName is the name the API server's certificate is verified
// against when K3S_SERVER_URL rewrites the address.
//
// k3s issues its serving certificate for the in-cluster service names, so
// every cluster carries this one — it is what in-cluster clients rely on. A
// cluster with an unusual --tls-san set can override it with
// K3S_TLS_SERVER_NAME.
const DefaultTLSServerName = "kubernetes.default.svc.cluster.local"

// Options configures the Kubernetes client.
type Options struct {
	// KubeconfigPath is the kubeconfig to load; empty means in-cluster config.
	KubeconfigPath string

	// ServerURL overrides the API server address from the kubeconfig. Needed
	// when the API runs in Docker and the kubeconfig points at 127.0.0.1.
	ServerURL string

	// TLSServerName is the name ServerURL's certificate is verified against.
	// Empty uses DefaultTLSServerName. Ignored unless ServerURL is set.
	TLSServerName string

	// SkipTLSVerify disables certificate verification entirely. See the warning
	// in NewClientWithOptions before setting it.
	SkipTLSVerify bool
}

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
	opts := Options{KubeconfigPath: kubeconfigPath}
	if len(serverURL) > 0 {
		opts.ServerURL = serverURL[0]
	}
	return NewClientWithOptions(opts)
}

// NewClientWithOptions builds the clientset and its REST config.
//
// On the TLS handling when ServerURL is set: the override changes how the API
// server is *addressed* (container → host), not which cluster it is, so the
// kubeconfig's CA still signs the certificate that will be presented. What the
// rewrite does break is hostname verification, because the k3s serving
// certificate is issued for the in-cluster names and the node address, not for
// a name like host.meshploy.internal.
//
// This previously resolved that by setting Insecure and discarding the CA,
// which silently disabled authentication of the connection on the standard
// Docker deployment — the one deploy/docker-compose.yml configures by default.
// That connection carries cluster-admin credentials. Pinning ServerName instead
// keeps the chain verified against the cluster CA while tolerating the address
// rewrite, which is what was actually needed.
func NewClientWithOptions(opts Options) (*kubernetes.Clientset, *rest.Config, error) {
	var cfg *rest.Config
	var err error

	if opts.KubeconfigPath != "" {
		cfg, err = clientcmd.BuildConfigFromFlags("", opts.KubeconfigPath)
	} else {
		cfg, err = rest.InClusterConfig()
	}
	if err != nil {
		return nil, nil, fmt.Errorf("k8s config: %w", err)
	}

	if opts.ServerURL != "" {
		cfg.Host = opts.ServerURL

		switch {
		case opts.SkipTLSVerify:
			// Insecure and a CA are mutually exclusive in client-go, so the CA
			// has to go for this to build at all.
			cfg.TLSClientConfig.Insecure = true
			cfg.TLSClientConfig.CAData = nil
			cfg.TLSClientConfig.CAFile = ""
			log.Printf("warning: K3S_SKIP_TLS_VERIFY is set — the connection to %s is encrypted but NOT authenticated. "+
				"Anything that can intercept it can read the cluster-admin credentials this API sends. "+
				"Unset it once K3S_TLS_SERVER_NAME matches a name on the cluster certificate.", opts.ServerURL)

		case cfg.TLSClientConfig.ServerName == "":
			name := opts.TLSServerName
			if name == "" {
				name = DefaultTLSServerName
			}
			cfg.TLSClientConfig.ServerName = name
		}
	}

	cs, err := kubernetes.NewForConfig(cfg)
	return cs, cfg, err
}

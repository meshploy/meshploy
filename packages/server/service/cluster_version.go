package service

import (
	"context"
	"regexp"
)

// k3sVersion is a k3s release as its installer takes it: v1.33.4+k3s1.
var k3sVersion = regexp.MustCompile(`^v\d+\.\d+\.\d+\+k3s\d+$`)

// ClusterVersion is the k3s release the cluster's server runs, which a node
// joining it installs. Left to itself the installer takes the day's stable
// release, and a node that joined months after the gateway ran a newer
// Kubernetes than its control plane, which Kubernetes does not support. Empty
// without a cluster, or when the server reports something that is not a k3s
// release: the installer then picks, as it did before.
func (s *NodeService) ClusterVersion(ctx context.Context) string {
	if s.k8s == nil {
		return ""
	}
	v, err := s.k8s.Discovery().ServerVersion()
	if err != nil || !k3sVersion.MatchString(v.GitVersion) {
		return ""
	}
	return v.GitVersion
}

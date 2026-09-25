package service

import "k8s.io/client-go/kubernetes"

// UseK8sForTest gives the deployment service a cluster client, so tests in
// service_test can drive a deploy against a fake one. Compiled only into tests.
func UseK8sForTest(s *Services, client kubernetes.Interface) {
	s.Deployments.k8s = client
}

package k8s

import corev1 "k8s.io/api/core/v1"

// appDNSConfig is the DNS an app's or a job's pod resolves with: the
// cluster's, as every pod's, with ndots at 2 instead of Kubernetes' 5.
//
// With ndots:5 a name with fewer than five dots is first tried with each of
// the pod's three cluster suffixes, so every lookup of an outside API
// (api.example.com) asked the cluster's DNS three questions that could only
// fail before the real one, adding tens to hundreds of milliseconds and, on a
// busy node, seconds. With 2, a name of two or more dots goes out as it is
// first. The names that mean something inside the cluster still resolve: a
// bare service name (postgres) or service.namespace has fewer than two dots
// and is tried with the suffixes first, and the full names Meshploy hands
// out (api.shop.svc.cluster.local) are tried as they are.
func appDNSConfig() *corev1.PodDNSConfig {
	two := "2"
	return &corev1.PodDNSConfig{Options: []corev1.PodDNSConfigOption{{Name: "ndots", Value: &two}}}
}

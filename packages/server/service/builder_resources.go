package service

import (
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	appk8s "github.com/meshploy/packages/server/k8s"
)

// ErrInvalidResources marks a resource value the cluster would reject, so the
// handler answers 400 rather than 500.
var ErrInvalidResources = errors.New("invalid resources")

// validateBuilderResources checks a build's requests and limits before they are
// saved: each must be a Kubernetes quantity, and a limit may not be below its
// request, which the cluster would refuse when the build starts.
func validateBuilderResources(cpuReq, cpuLim, memReq, memLim string) error {
	for _, r := range []struct{ what, req, lim, defReq, example string }{
		{"CPU", cpuReq, cpuLim, appk8s.DefaultBuilderCPURequest, "1000m or 2"},
		{"memory", memReq, memLim, appk8s.DefaultBuilderMemoryRequest, "1Gi or 512Mi"},
	} {
		req := resource.MustParse(r.defReq)
		if r.req != "" {
			q, err := resource.ParseQuantity(r.req)
			if err != nil {
				return fmt.Errorf("%w: the builder %s request %q is not a Kubernetes quantity, such as %s", ErrInvalidResources, r.what, r.req, r.example)
			}
			req = q
		}
		if r.lim == "" {
			continue
		}
		lim, err := resource.ParseQuantity(r.lim)
		if err != nil {
			return fmt.Errorf("%w: the builder %s limit %q is not a Kubernetes quantity, such as %s", ErrInvalidResources, r.what, r.lim, r.example)
		}
		if lim.Cmp(req) < 0 {
			return fmt.Errorf("%w: the builder %s limit %s is below its request %s", ErrInvalidResources, r.what, r.lim, req.String())
		}
	}
	return nil
}

// buildOutOfMemoryMessage explains a build stopped at its memory limit.
func buildOutOfMemoryMessage(memReq, memLim string) string {
	limit := appk8s.BuilderResources(appk8s.BuildJobParams{MemoryRequest: memReq, MemoryLimit: memLim}).Limits[corev1.ResourceMemory]
	return fmt.Sprintf("The build ran out of memory: it reached its %s limit and was stopped. Raise the builder memory limit in the service's Build settings.", limit.String())
}

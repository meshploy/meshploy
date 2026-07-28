package k8s

import (
	"context"

	batchv1 "k8s.io/api/batch/v1"
)

// Extension point: mutate Kubernetes Jobs before they are submitted.
//
// This lives in the k8s package rather than the service layer because the Job
// object is constructed here — CreateBuildJob, CreateRunJob and ApplyCronJob are
// the only three places one is built, and a mutator must see the final object
// immediately before the API call. Hooking at the service layer would miss two
// of the three and observe an unbuilt spec.
//
// EE's MSP mode uses this for per-org build isolation: rewriting the namespace
// and attaching NetworkPolicy so one customer's build cannot reach another's.
// jobMutators stays empty in CE builds.

// JobKind distinguishes the Job paths so a mutator can target one.
//
// ApplyCronJob is deliberately not covered: it submits a batchv1.CronJob, a
// different type whose pod template lives at Spec.JobTemplate.Spec. Wiring it
// through this interface would mean lying about the type. When a feature needs
// cron isolation, add a sibling CronJobMutator rather than widening this one.
type JobKind string

const (
	JobBuild JobKind = "build" // image build for a deployment
	JobRun   JobKind = "run"   // one-off job execution
)

// JobMutator adjusts a Job before submission. It receives the fully built
// object and may modify it in place. Returning an error aborts the submission,
// so a mutator that cannot satisfy its policy fails closed rather than allowing
// an unisolated Job to run.
type JobMutator interface {
	MutateJob(ctx context.Context, kind JobKind, job *batchv1.Job) error
}

var jobMutators []JobMutator

// RegisterJobMutator adds a Job mutator. Call from an extension's init():
//
//	func init() { k8s.RegisterJobMutator(buildIsolation{}) }
func RegisterJobMutator(jm JobMutator) {
	jobMutators = append(jobMutators, jm)
}

// applyJobMutators runs every registered mutator in order. A no-op in CE builds.
func applyJobMutators(ctx context.Context, kind JobKind, job *batchv1.Job) error {
	for _, jm := range jobMutators {
		if err := jm.MutateJob(ctx, kind, job); err != nil {
			return err
		}
	}
	return nil
}

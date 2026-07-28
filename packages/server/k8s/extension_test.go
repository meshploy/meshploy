package k8s

import (
	"context"
	"errors"
	"testing"

	batchv1 "k8s.io/api/batch/v1"
)

type fakeMutator struct {
	seen   []JobKind
	reject error
	stamp  string
}

func (f *fakeMutator) MutateJob(_ context.Context, kind JobKind, job *batchv1.Job) error {
	f.seen = append(f.seen, kind)
	if f.reject != nil {
		return f.reject
	}
	if f.stamp != "" {
		if job.Labels == nil {
			job.Labels = map[string]string{}
		}
		job.Labels["mutated-by"] = f.stamp
	}
	return nil
}

func withMutators(t *testing.T, ms ...JobMutator) {
	t.Helper()
	saved := jobMutators
	t.Cleanup(func() { jobMutators = saved })
	jobMutators = nil
	for _, m := range ms {
		RegisterJobMutator(m)
	}
}

// The load-bearing CE guarantee: nothing registered, Jobs pass through untouched.
func TestJobMutatorNoopInCE(t *testing.T) {
	withMutators(t)
	job := &batchv1.Job{}
	if err := applyJobMutators(context.Background(), JobBuild, job); err != nil {
		t.Fatalf("CE build must not mutate Jobs: %v", err)
	}
	if len(job.Labels) != 0 {
		t.Fatalf("Job must be untouched in a CE build, got labels %v", job.Labels)
	}
}

// A mutator sees the real object and its edits persist — this is what per-org
// build isolation relies on.
func TestJobMutatorMutatesInPlace(t *testing.T) {
	withMutators(t, &fakeMutator{stamp: "ee"})
	job := &batchv1.Job{}
	if err := applyJobMutators(context.Background(), JobBuild, job); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if job.Labels["mutated-by"] != "ee" {
		t.Fatalf("mutator edits must persist on the submitted Job, got %v", job.Labels)
	}
}

// Build and run Jobs are distinguishable so a mutator can target only builds.
func TestJobMutatorReceivesKind(t *testing.T) {
	f := &fakeMutator{}
	withMutators(t, f)
	ctx := context.Background()
	_ = applyJobMutators(ctx, JobBuild, &batchv1.Job{})
	_ = applyJobMutators(ctx, JobRun, &batchv1.Job{})

	if len(f.seen) != 2 || f.seen[0] != JobBuild || f.seen[1] != JobRun {
		t.Fatalf("want [build run], got %v", f.seen)
	}
}

// Fail closed: a mutator that cannot satisfy its isolation policy must abort
// submission rather than let an unisolated Job run.
func TestJobMutatorErrorAborts(t *testing.T) {
	boom := errors.New("no namespace available for org")
	second := &fakeMutator{}
	withMutators(t, &fakeMutator{reject: boom}, second)

	if err := applyJobMutators(context.Background(), JobBuild, &batchv1.Job{}); !errors.Is(err, boom) {
		t.Fatalf("want the mutator's error to abort submission, got %v", err)
	}
	if len(second.seen) != 0 {
		t.Fatal("a mutator after a failure must not run")
	}
}

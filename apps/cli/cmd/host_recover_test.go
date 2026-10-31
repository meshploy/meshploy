package cmd

import (
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/meshploy/apps/cli/internal/dockerapi"
)

func detail(status, failure string) dockerapi.ContainerDetail {
	var d dockerapi.ContainerDetail
	d.State.Status, d.State.Error = status, failure
	return d
}

// The error is Docker's own, as a gateway's API carried it after a reboot.
const bootFailure = "failed to set up container networking: driver failed programming external connectivity on endpoint meshploy-api-1 (7809): failed to bind host port 100.64.0.1:4000/tcp: cannot assign requested address"

func TestOnlyAFailedBindCounts(t *testing.T) {
	for name, c := range map[string]struct {
		d    dockerapi.ContainerDetail
		want bool
	}{
		"failed at boot":     {detail("exited", bootFailure), true},
		"never started":      {detail("created", bootFailure), true},
		"stopped by someone": {detail("exited", ""), false},
		"crashed on its own": {detail("exited", "exit status 1"), false},
		"a stale error":      {detail("running", bootFailure), false},
		"restarting":         {detail("restarting", bootFailure), false},
	} {
		if got := bindFailed(c.d); got != c.want {
			t.Errorf("%s: %v, want %v", name, got, c.want)
		}
	}
}

func recovery(stuck []string, mesh bool) (*recoverer, *[][]string) {
	var ran [][]string
	r := &recoverer{logw: io.Discard,
		stuck:    func(context.Context) ([]string, error) { return stuck, nil },
		meshUp:   func() bool { return mesh },
		recreate: func(s []string) error { ran = append(ran, s); return nil },
	}
	return r, &ran
}

func TestStuckServicesAreRecreatedOnceTheMeshIsUp(t *testing.T) {
	now := time.Now()
	r, ran := recovery([]string{"api", "registry"}, false)
	if _, acted := r.check(context.Background(), now); acted || len(*ran) != 0 {
		t.Fatalf("acted before the mesh had its address: %v", *ran)
	}
	r.meshUp = func() bool { return true }
	task, acted := r.check(context.Background(), now.Add(recoverInterval))
	if !acted || !task.OK || !reflect.DeepEqual(*ran, [][]string{{"api", "registry"}}) {
		t.Fatalf("%v, %+v, %v", acted, task, *ran)
	}
}

func TestNothingStuckIsNothingToDoOrSay(t *testing.T) {
	r, ran := recovery(nil, true)
	if _, acted := r.check(context.Background(), time.Now()); acted || len(*ran) != 0 {
		t.Errorf("acted on nothing: %v", *ran)
	}
	r.stuck = func(context.Context) ([]string, error) { return nil, errors.New("no socket yet") }
	if _, acted := r.check(context.Background(), time.Now()); acted {
		t.Error("a runtime still starting was reported")
	}
}

// A recreate that fails says why, and is tried again after a while, not on
// every look.
func TestAFailedRecreateWaitsAndSaysWhy(t *testing.T) {
	now := time.Now()
	r, _ := recovery([]string{"api"}, true)
	tries := 0
	r.recreate = func([]string) error { tries++; return errors.New("port in use") }
	task, acted := r.check(context.Background(), now)
	if !acted || task.OK || !strings.Contains(task.Error, "port in use") {
		t.Fatalf("%v, %+v", acted, task)
	}
	r.check(context.Background(), now.Add(recoverInterval))
	if tries != 1 {
		t.Errorf("tried %d times within the wait", tries)
	}
	r.check(context.Background(), now.Add(recoverRetry))
	if tries != 2 {
		t.Errorf("tried %d times after the wait", tries)
	}
}

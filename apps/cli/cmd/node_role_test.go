package cmd

import "testing"

// --role takes the installer's four worker roles, or nothing for its default.
func TestCheckNodeRole(t *testing.T) {
	for _, ok := range []string{"", "workload_builder", "workload", "builder", "mesh"} {
		if err := checkNodeRole(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"meshonly", "Mesh", "worker", "4"} {
		if err := checkNodeRole(bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
}

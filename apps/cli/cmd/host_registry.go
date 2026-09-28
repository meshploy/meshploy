package cmd

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/meshploy/packages/hostagent"
)

// registryConfig is the configuration file the registry image ships with,
// which the gateway's compose file configures through its environment.
const registryConfig = "/etc/docker/registry/config.yml"

// runRegistryGC runs the built-in registry's garbage collection, and reports
// how much it held before and after.
//
// Without --delete-untagged: the images Meshploy removes are removed by
// manifest, which this frees, and that flag has deleted the images of a
// multi-platform index still in use.
func runRegistryGC() ([]byte, error) {
	runtime := detectContainerRuntime()
	before, err := registryBytes(runtime)
	if err != nil {
		return nil, err
	}
	if out, err := runtimeOutput(meshployInstDir, runtime, "compose", "exec", "-T", "registry",
		"registry", "garbage-collect", registryConfig); err != nil {
		return nil, fmt.Errorf("registry garbage-collect: %w: %s", err, lastLines(string(out), 3))
	}
	after, err := registryBytes(runtime)
	if err != nil {
		return nil, err
	}
	return json.Marshal(hostagent.RegistryGC{FinishedAt: time.Now().UTC(), BeforeBytes: before, AfterBytes: after})
}

// registryBytes is the size of the registry's storage.
func registryBytes(runtime string) (int64, error) {
	out, err := runtimeOutput(meshployInstDir, runtime, "compose", "exec", "-T", "registry",
		"du", "-sk", "/var/lib/registry")
	if err != nil {
		return 0, fmt.Errorf("measure the registry: %w", err)
	}
	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		return 0, fmt.Errorf("measure the registry: no output")
	}
	kb, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("measure the registry: %q", fields[0])
	}
	return kb * 1024, nil
}

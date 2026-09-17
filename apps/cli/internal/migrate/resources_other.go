//go:build !linux

package migrate

import "runtime"

// ReadResources measures cores only: migrating runs on a Linux server, and the
// CLI builds for other systems for everything else it does.
func ReadResources(docker Docker) Resources {
	return Resources{Cores: runtime.NumCPU(), DockerVolumeMB: docker.TotalMB()}
}

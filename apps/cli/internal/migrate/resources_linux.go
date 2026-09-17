package migrate

import (
	"os"
	"runtime"
	"syscall"
)

// ReadResources measures this host. docker is the inventory already read, for
// the size of its volumes.
func ReadResources(docker Docker) Resources {
	res := Resources{Cores: runtime.NumCPU(), DockerVolumeMB: docker.TotalMB()}
	if b, err := os.ReadFile("/proc/meminfo"); err == nil {
		res.MemoryMB, res.AvailableMB = ParseMeminfo(string(b))
	}
	var st syscall.Statfs_t
	if syscall.Statfs("/", &st) == nil {
		res.DiskFreeMB = int(st.Bavail * uint64(st.Bsize) / (1 << 20))
	}
	return res
}

package service

import (
	"fmt"
	"math"

	composetypes "github.com/compose-spec/compose-go/v2/types"
)

// stackResources is what a compose service declares of its resources, each
// empty where it declares nothing.
type stackResources struct {
	CPURequest, CPULimit, MemoryRequest, MemoryLimit string
}

// composeResources reads the resources a compose file gives Docker - mem_limit,
// cpus, mem_reservation and deploy.resources - as the ones a stack runs with.
// A file written for Docker says what its services need there, and a stack
// that ignored it gave every service Meshploy's defaults instead: a Redpanda
// told to take 1 GiB could not start inside a 1 GiB limit.
func composeResources(def composetypes.ServiceConfig) stackResources {
	var r stackResources
	if def.CPUS > 0 {
		r.CPULimit = millicores(float64(def.CPUS))
	}
	if def.MemLimit > 0 {
		r.MemoryLimit = fmt.Sprint(int64(def.MemLimit))
	}
	if def.MemReservation > 0 {
		r.MemoryRequest = fmt.Sprint(int64(def.MemReservation))
	}
	if def.Deploy != nil {
		if l := def.Deploy.Resources.Limits; l != nil {
			if l.NanoCPUs > 0 {
				r.CPULimit = millicores(float64(l.NanoCPUs))
			}
			if l.MemoryBytes > 0 {
				r.MemoryLimit = fmt.Sprint(int64(l.MemoryBytes))
			}
		}
		if q := def.Deploy.Resources.Reservations; q != nil {
			if q.NanoCPUs > 0 {
				r.CPURequest = millicores(float64(q.NanoCPUs))
			}
			if q.MemoryBytes > 0 {
				r.MemoryRequest = fmt.Sprint(int64(q.MemoryBytes))
			}
		}
	}
	return r
}

func millicores(cpus float64) string {
	return fmt.Sprintf("%dm", int64(math.Ceil(cpus*1000)))
}

// orKept is what the spec declares, or what the service already has where the
// spec declares nothing: a limit set in the console, or carried from the old
// platform by a migration, is not put back to a default by the next apply.
func orKept(declared, existing, fallback string) string {
	switch {
	case declared != "":
		return declared
	case existing != "":
		return existing
	}
	return fallback
}

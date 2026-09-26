package service

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
	appk8s "github.com/meshploy/packages/server/k8s"
)

// Why a service is not staying up, read from its pods.
//
// A pod that keeps dying still says "Running" most of the time: Kubernetes
// restarts it, and the reason lives in the container's last termination. So
// the console asks for this summary rather than trusting the status alone:
// an out-of-memory kill, a process that keeps exiting, an image that cannot
// be pulled, a container that cannot start. Old restarts do not count; only
// what happened in troubleWindow, or what is happening now.

// Trouble kinds, most specific first.
const (
	TroubleImagePull   = "image_pull"    // the image cannot be pulled
	TroubleCannotStart = "cannot_start"  // the container cannot be created: a missing secret, a bad command
	TroubleOutOfMemory = "out_of_memory" // killed for going over its memory limit
	TroubleCrashing    = "crashing"      // keeps exiting and being restarted
)

const troubleWindow = 30 * time.Minute

// Trouble is what is wrong with a service's pods, when something is.
type Trouble struct {
	Kind        string     `json:"kind"`
	Restarts    int32      `json:"restarts"`
	LastAt      *time.Time `json:"last_at,omitempty"`
	ExitCode    int32      `json:"exit_code,omitempty"`
	Message     string     `json:"message,omitempty"`
	MemoryLimit string     `json:"memory_limit,omitempty"`
}

// troubleOf reads one service's pods as of now; nil when they are fine.
func troubleOf(pods []appk8s.PodInfo, now time.Time) *Trouble {
	var t Trouble
	var oom, recentExit, crashLoop bool
	var imagePull, cannotStart string
	for _, p := range pods {
		t.Restarts += p.Restarts
		if p.MemoryLimit != "" {
			t.MemoryLimit = p.MemoryLimit
		}
		switch p.WaitingReason {
		case "ImagePullBackOff", "ErrImagePull", "InvalidImageName":
			imagePull = p.WaitingMessage
		case "CreateContainerConfigError", "CreateContainerError", "RunContainerError":
			cannotStart = p.WaitingMessage
		case "CrashLoopBackOff":
			crashLoop = true
		}
		if p.LastFinishedAt != nil && now.Sub(*p.LastFinishedAt) < troubleWindow {
			if t.LastAt == nil || p.LastFinishedAt.After(*t.LastAt) {
				t.LastAt, t.ExitCode = p.LastFinishedAt, p.LastExitCode
			}
			if p.LastReason == "OOMKilled" {
				oom = true
			} else if p.Restarts > 0 {
				recentExit = true
			}
		}
	}
	switch {
	case imagePull != "":
		t.Kind, t.Message = TroubleImagePull, imagePull
	case cannotStart != "":
		t.Kind, t.Message = TroubleCannotStart, cannotStart
	case oom:
		t.Kind = TroubleOutOfMemory
	case crashLoop || recentExit:
		t.Kind = TroubleCrashing
	default:
		return nil
	}
	return &t
}

// Troubles is every service in a project (or level) that is not staying up,
// by service id: one call to the cluster for the whole project.
func (s *WorkloadService) Troubles(ctx context.Context, projectID uuid.UUID) (map[uuid.UUID]*Trouble, error) {
	out := map[uuid.UUID]*Trouble{}
	if s.k8s == nil {
		return out, nil
	}
	var project db.Project
	if err := s.db.WithContext(ctx).Select("id", "slug").First(&project, "id = ?", projectID).Error; err != nil {
		return nil, err
	}
	var services []db.Service
	if err := s.db.WithContext(ctx).Preload("DatabaseConfig").Where("project_id = ?", projectID).Find(&services).Error; err != nil {
		return nil, err
	}
	if len(services) == 0 {
		return out, nil
	}
	pods, err := appk8s.ListManagedPods(ctx, s.k8s, project.Slug)
	if err != nil {
		return out, nil // no namespace yet: nothing running, nothing wrong
	}
	byApp := map[string][]appk8s.PodInfo{}
	for _, p := range pods {
		byApp[p.App] = append(byApp[p.App], p)
	}
	now := time.Now()
	for i := range services {
		if t := troubleOf(byApp[s.k8sName(ctx, &services[i])], now); t != nil {
			out[services[i].ID] = t
		}
	}
	return out, nil
}

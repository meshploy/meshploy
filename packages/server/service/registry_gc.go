package service

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
	"github.com/meshploy/packages/hostagent"
)

// Removing an image from the registry removes its manifest; its layers stay on
// disk until the registry's garbage collection runs, which needs the host (the
// API has neither the registry's files nor a container runtime). So the API
// asks the host agent for one, at most weekly, once images have been removed
// since the last, and never while a build may be pushing.

const (
	registryGCEvery = 7 * 24 * time.Hour
	registryGCCheck = time.Hour
)

// StartRegistryGC asks for the built-in registry's garbage collection when it
// is due, until ctx ends.
func (s *DeploymentService) StartRegistryGC(ctx context.Context) {
	t := time.NewTicker(registryGCCheck)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := s.requestRegistryGC(ctx, time.Now()); err != nil {
				log.Printf("warning: registry garbage collection: %v", err)
			}
		}
	}
}

// requestRegistryGC files a registry.gc request when one is due.
func (s *DeploymentService) requestRegistryGC(ctx context.Context, now time.Time) error {
	if s.cfg == nil || s.cfg.HostDir == "" || s.cfg.BuiltinRegistryEndpoint == "" {
		return nil
	}
	inbox := hostagent.InboxDir(s.cfg.HostDir)
	if _, err := os.Stat(inbox); err != nil {
		return nil // no host agent on this machine
	}
	entries, err := os.ReadDir(inbox)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), hostagent.RequestRegistryGC+"-") {
			return nil // asked already, not yet taken
		}
	}
	var last time.Time
	if b, err := os.ReadFile(filepath.Join(hostagent.MigrateDir(s.cfg.HostDir), hostagent.RegistryGCFile)); err == nil {
		var gc hostagent.RegistryGC
		if json.Unmarshal(b, &gc) == nil {
			last = gc.FinishedAt
		}
	}
	if !last.IsZero() && now.Sub(last) < registryGCEvery {
		return nil
	}
	var removed int64
	s.db.WithContext(ctx).Model(&db.Deployment{}).Where("image_removed_at > ?", last).Count(&removed)
	if removed == 0 {
		return nil
	}
	var building int64
	s.db.WithContext(ctx).Model(&db.Deployment{}).
		Where("status IN ?", []db.DeploymentStatus{db.DeploymentPending, db.DeploymentBuilding}).Count(&building)
	if building > 0 {
		return nil // an hour from now, once no build is pushing
	}
	return queueHostRequest(s.cfg.HostDir, hostagent.Request{
		ID: uuid.NewString(), Type: hostagent.RequestRegistryGC, RequestedBy: "meshploy", RequestedAt: now.UTC(),
	})
}

// queueHostRequest leaves a request in the host agent's inbox. It is written
// under a temporary name and renamed, so the agent never reads half of one.
func queueHostRequest(hostDir string, req hostagent.Request) error {
	if err := hostagent.ValidateRequest(req, ""); err != nil {
		return err
	}
	data, err := json.Marshal(req)
	if err != nil {
		return err
	}
	inbox := hostagent.InboxDir(hostDir)
	tmp, err := os.CreateTemp(inbox, ".request-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(inbox, hostagent.RequestFileName(req)))
}

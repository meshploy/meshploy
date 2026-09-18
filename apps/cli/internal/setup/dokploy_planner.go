package setup

import (
	"context"

	"github.com/meshploy/apps/cli/internal/migrate"
	"github.com/meshploy/apps/cli/internal/migrate/dokploy"
	"time"
)

// DokployPlanner reads this host for Dokploy. Everything it does is read-only.
type DokployPlanner struct{}

func (DokployPlanner) Detect(context.Context) (dokploy.Plan, error) {
	src, err := dokploy.CollectDetect(migrate.ExecRunner{})
	if err != nil && !src.Detection.Dokploy {
		return dokploy.Plan{}, err
	}
	return dokploy.BuildPlan(src, time.Now()), nil
}

func (DokployPlanner) Plan(context.Context) (dokploy.Plan, error) {
	src, err := dokploy.Collect(migrate.ExecRunner{})
	if err != nil {
		return dokploy.Plan{}, err
	}
	return dokploy.BuildPlan(src, time.Now()), nil
}

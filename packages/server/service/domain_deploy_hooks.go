package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
)

// A service's deploy webhook is a URL someone pasted into their CI. Meshploy
// never registered it anywhere, so it cannot know where it lives or change it.
// What it can see is where each call arrives: the Host a CI job used. A domain
// that CI jobs still call through cannot be removed without their deploys
// failing, so those calls hold it - and once a job is updated, its next call
// arrives through the new name and the hold clears by itself.

// RecordDeployHookCall notes the host a deploy webhook call arrived through.
// Called only after the token was accepted: an unauthenticated caller writes
// the Host header too, and must not be able to add or clear anything here.
func (s *DomainService) RecordDeployHookCall(ctx context.Context, serviceID uuid.UUID, host string) error {
	host = strings.ToLower(host)
	if i := strings.LastIndexByte(host, ':'); i >= 0 {
		host = host[:i]
	}
	now := time.Now().UTC()
	return s.db.WithContext(ctx).Model(&db.BuildConfig{}).Where("service_id = ?", serviceID).
		Updates(db.BuildConfig{DeployHookHost: host, DeployHookCalledAt: &now}).Error
}

// DeployHook is a service whose deploy webhook was last called through a
// domain.
type DeployHook struct {
	ServiceID   uuid.UUID  `json:"service_id"`
	ServiceName string     `json:"service_name"`
	ProjectID   uuid.UUID  `json:"project_id"`
	ProjectName string     `json:"project_name"`
	Host        string     `json:"host"`
	CalledAt    *time.Time `json:"called_at"`
}

// DeployHooksOnDomain lists the services whose deploy webhook was last called
// through this domain's api. or console. name. Only a domain that is or was
// primary serves those names, so only such a domain can have any.
//
// A webhook never called is not listed: nothing shows it is in use. One called
// long ago is, because a CI job that runs once a month is still a CI job.
func (s *DomainService) DeployHooksOnDomain(ctx context.Context, orgID uuid.UUID, domain *db.Domain) ([]DeployHook, error) {
	out := make([]DeployHook, 0)
	if !domain.IsPrimary && !domain.FormerPrimary {
		return out, nil
	}
	err := s.db.WithContext(ctx).Table("build_configs").
		Select(`build_configs.service_id, services.name AS service_name,
		        projects.id AS project_id, projects.name AS project_name,
		        build_configs.deploy_hook_host AS host, build_configs.deploy_hook_called_at AS called_at`).
		Joins("JOIN services ON services.id = build_configs.service_id").
		Joins("JOIN projects ON projects.id = services.project_id").
		Where("projects.organization_id = ? AND build_configs.deploy_hook_host IN ?",
			orgID, []string{"api." + domain.BaseDomain, "console." + domain.BaseDomain}).
		Order("services.name ASC").
		Scan(&out).Error
	return out, err
}

// ForgetDeployHookCall clears the record for a CI job that no longer exists.
//
// For the one case watching cannot settle: a job that called through the old
// domain and was since deleted will never call again. If it does call again,
// it is recorded again, so clearing a live one by mistake undoes itself.
func (s *DomainService) ForgetDeployHookCall(ctx context.Context, orgID, serviceID uuid.UUID) error {
	var n int64
	s.db.WithContext(ctx).Table("services").
		Joins("JOIN projects ON projects.id = services.project_id").
		Where("services.id = ? AND projects.organization_id = ?", serviceID, orgID).Count(&n)
	if n == 0 {
		return huma.Error404NotFound("service not found")
	}
	return s.db.WithContext(ctx).Model(&db.BuildConfig{}).Where("service_id = ?", serviceID).
		Updates(map[string]any{"deploy_hook_host": "", "deploy_hook_called_at": nil}).Error
}

func errDeployHooksOnDomain(n int, domain string) error {
	what := "CI deploy webhooks were"
	if n == 1 {
		what = "CI deploy webhook was"
	}
	return huma.Error422UnprocessableEntity(fmt.Sprintf(
		"%d %s last called through %s - update the URL in the CI job, or its deploys fail when this domain goes",
		n, what, domain))
}

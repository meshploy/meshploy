package service_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A git connection reserved for one project is built from by that project and
// its levels only; one that is the organization's serves every project. The
// browser returning from GitHub goes to the door on the project's own domain.
func TestAGitConnectionReservedForOneProject(t *testing.T) {
	service.RegisterConsoleName("gitdoor")
	svc, gdb, org, _ := edgeServer(t)
	ctx := context.Background()
	acme, err := svc.Projects.Create(ctx, org, "Acme", "acme-"+uuid.NewString()[:6])
	require.NoError(t, err)
	other, err := svc.Projects.Create(ctx, org, "Other", "other-"+uuid.NewString()[:6])
	require.NoError(t, err)
	staging, err := svc.Projects.CreateLevel(ctx, acme.ID, "staging", acme.ID, false)
	require.NoError(t, err)

	reserved := db.GitIntegration{OrganizationID: org, Provider: "github", AuthMethod: "app", Name: "acme's", ProjectID: &acme.ID}
	shared := db.GitIntegration{OrganizationID: org, Provider: "github", AuthMethod: "app", Name: "the org's"}
	require.NoError(t, gdb.Create(&reserved).Error)
	require.NoError(t, gdb.Create(&shared).Error)

	assert.NoError(t, svc.GitIntegrations.UsableIn(ctx, org, reserved.ID, acme.ID), "its own project")
	assert.NoError(t, svc.GitIntegrations.UsableIn(ctx, org, reserved.ID, staging.ID), "and its levels")
	assert.ErrorIs(t, svc.GitIntegrations.UsableIn(ctx, org, reserved.ID, other.ID), service.ErrGitIntegrationReserved)
	assert.NoError(t, svc.GitIntegrations.UsableIn(ctx, org, shared.ID, other.ID), "the org's serves every project")
	assert.ErrorIs(t, svc.GitIntegrations.UsableIn(ctx, uuid.New(), shared.ID, other.ID), service.ErrGitIntegrationNotFound)

	// Made through a level, it is reserved for the level's project, and records
	// who made it.
	maker := uuid.New()
	row, _, _, err := svc.GitIntegrations.InitGitHubIntegration(ctx, org, "", "gitdoor", &staging.ID, &maker)
	require.NoError(t, err)
	require.NotNil(t, row.ProjectID)
	assert.Equal(t, acme.ID, *row.ProjectID)
	require.NotNil(t, row.CreatedBy)
	assert.Equal(t, maker, *row.CreatedBy, "who made it is recorded")

	assert.Empty(t, svc.Domains.ProjectConsoleURL(ctx, acme.ID, "gitdoor"), "no domain reserved yet")
	d, err := svc.Domains.Create(ctx, org, "acme-"+uuid.NewString()[:6]+".example.com", db.DNSModeOnDemand)
	require.NoError(t, err)
	require.NoError(t, gdb.Model(&db.Domain{}).Where("id = ?", d.ID).Update("verified", true).Error)
	_, err = svc.Domains.ReserveForProject(ctx, d.ID, &acme.ID)
	require.NoError(t, err)
	assert.Equal(t, "https://gitdoor."+d.BaseDomain, svc.Domains.ProjectConsoleURL(ctx, staging.ID, "gitdoor"))
	assert.Empty(t, svc.Domains.ProjectConsoleURL(ctx, acme.ID, "console"), "the platform's own console stays on the primary")

	// Deleting the project takes its connection with it.
	require.NoError(t, svc.Promotions.DeleteProject(ctx, staging.ID))
	_, err = svc.Domains.ReserveForProject(ctx, d.ID, nil)
	require.NoError(t, err)
	require.NoError(t, svc.Promotions.DeleteProject(ctx, acme.ID))
	var n int64
	gdb.Model(&db.GitIntegration{}).Where("project_id = ?", acme.ID).Count(&n)
	assert.Zero(t, n)
}

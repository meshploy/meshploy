package service_test

import (
	"context"
	"testing"
	"time"

	meshdb "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The projects page searches and sorts on the server: a search matches name or
// slug ignoring case, treats LIKE's wildcards as plain characters, and the list
// comes back newest first or by name.
func TestProjectListSearchesAndSorts(t *testing.T) {
	ctx := context.Background()
	gdb := newTestDB(t)
	svcs := newServices(gdb)

	org := meshdb.Organization{Name: "list", Slug: "list"}
	require.NoError(t, gdb.Create(&org).Error)
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	for i, p := range []struct{ name, slug string }{
		{"beta", "beta"},
		{"Alpha", "alpha"},
		{"Gamma_x", "gamma-x"},
		{"Payments", "billing"},
	} {
		created, err := svcs.Projects.Create(ctx, org.ID, p.name, p.slug)
		require.NoError(t, err)
		require.NoError(t, gdb.Model(&meshdb.Project{}).Where("id = ?", created.ID).
			Update("created_at", base.Add(time.Duration(i)*time.Hour)).Error)
	}

	names := func(opts service.ProjectListOptions) []string {
		t.Helper()
		list, err := svcs.Projects.ListWithCounts(ctx, org.ID, opts)
		require.NoError(t, err)
		out := make([]string, len(list))
		for i, p := range list {
			out[i] = p.Name
		}
		return out
	}

	assert.Equal(t, []string{"Payments", "Gamma_x", "Alpha", "beta"}, names(service.ProjectListOptions{}), "newest first by default")
	assert.Equal(t, []string{"Alpha", "beta", "Gamma_x", "Payments"}, names(service.ProjectListOptions{Sort: "name"}), "A to Z, ignoring case")
	assert.Equal(t, []string{"Alpha"}, names(service.ProjectListOptions{Search: "ALP"}), "ignores case")
	assert.Equal(t, []string{"Payments"}, names(service.ProjectListOptions{Search: "bill"}), "matches the slug")
	assert.Equal(t, []string{"Gamma_x"}, names(service.ProjectListOptions{Search: "_"}), "an underscore is not a wildcard")
	assert.Empty(t, names(service.ProjectListOptions{Search: "%"}), "a percent sign is not a wildcard")
	assert.Equal(t, []string{"Gamma_x", "Alpha"}, names(service.ProjectListOptions{Search: "a", Sort: "recent"})[1:3], "search and sort combine")
}

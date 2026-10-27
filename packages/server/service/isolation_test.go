package service_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
	appk8s "github.com/meshploy/packages/server/k8s"
	"github.com/meshploy/packages/server/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// A project's namespace is open to its other levels, and to a project whose
// service or job uses one of its services' published variables; never to a
// project it merely shares a workspace with.
func TestWhoMayReachAProject(t *testing.T) {
	gdb := newTestDB(t)
	ctx := context.Background()
	id := func() db.Base { return db.Base{ID: uuid.New()} }
	org := db.Organization{Base: id(), Name: "acme", Slug: "acme-" + uuid.NewString()[:8]}
	shop := db.Project{Base: id(), OrganizationID: org.ID, Name: "Shop", Slug: "shop"}
	staging := db.Project{Base: id(), OrganizationID: org.ID, Name: "Shop", Slug: "shop-staging", ParentProjectID: &shop.ID, EnvLevel: 1}
	tools := db.Project{Base: id(), OrganizationID: org.ID, Name: "Tools", Slug: "tools"}
	reports := db.Project{Base: id(), OrganizationID: org.ID, Name: "Reports", Slug: "reports"}
	lonely := db.Project{Base: id(), OrganizationID: org.ID, Name: "Lonely", Slug: "lonely"}
	postgres := db.Service{Base: id(), ProjectID: shop.ID, Name: "postgres", Slug: "postgres", Type: db.ServiceTypeDatabase}
	dashboard := db.Service{Base: id(), ProjectID: tools.ID, Name: "dashboard", Slug: "dashboard"}
	published := db.VariableGroup{Base: id(), ProjectID: shop.ID, ServiceID: &postgres.ID, Name: "postgres", SystemManaged: true}
	export := db.Job{Base: id(), ProjectID: reports.ID, Name: "export"}
	for _, row := range []any{&org, &shop, &staging, &tools, &reports, &lonely, &postgres, &dashboard, &published, &export,
		&db.ServiceVariableGroup{Base: id(), ServiceID: dashboard.ID, GroupID: published.ID},
		&db.JobVariableGroup{Base: id(), JobID: export.ID, GroupID: published.ID}} {
		require.NoError(t, gdb.Create(row).Error)
	}

	got, err := service.NewIsolationForTest(gdb, nil).Neighbours(ctx)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"shop", "shop-staging", "tools", "reports"}, got["shop"],
		"its level, and the projects that use its database")
	assert.ElementsMatch(t, []string{"shop", "shop-staging"}, got["shop-staging"], "a level and its project")
	assert.ElementsMatch(t, []string{"tools"}, got["tools"], "using a service opens nothing in the user's own namespace")
	assert.ElementsMatch(t, []string{"lonely"}, got["lonely"])
}

// A sweep isolates every existing namespace of an organization that has
// isolation on, except a project opened on its own, leaves other
// organizations' alone, and switched off takes every policy it made away.
func TestASweepFollowsEachOrganizationsSwitch(t *testing.T) {
	gdb := newTestDB(t)
	ctx := context.Background()
	acme := db.Organization{Name: "acme", Slug: "acme-" + uuid.NewString()[:8]}
	other := db.Organization{Name: "other", Slug: "other-" + uuid.NewString()[:8]}
	require.NoError(t, gdb.Create(&acme).Error)
	require.NoError(t, gdb.Create(&other).Error)
	require.NoError(t, gdb.Create(&db.Project{OrganizationID: acme.ID, Name: "Shop", Slug: "shop"}).Error)
	require.NoError(t, gdb.Create(&db.Project{OrganizationID: acme.ID, Name: "Later", Slug: "later"}).Error)
	require.NoError(t, gdb.Create(&db.Project{OrganizationID: other.ID, Name: "Blog", Slug: "blog"}).Error)
	meshploy := map[string]string{"managed-by": "meshploy"}
	client := fake.NewSimpleClientset(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "shop", Labels: meshploy}},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "blog", Labels: meshploy}},
		&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "gw"}, Spec: corev1.NodeSpec{PodCIDR: "10.42.0.0/24"}},
	)
	isolation := service.NewIsolationForTest(gdb, client)
	policies := client.NetworkingV1().NetworkPolicies

	require.NoError(t, isolation.Sweep(ctx))
	left, _ := policies("").List(ctx, metav1.ListOptions{})
	assert.Empty(t, left.Items, "off by default")

	require.NoError(t, isolation.Set(ctx, acme.ID, true))
	p, err := policies("shop").Get(ctx, appk8s.IsolationPolicyName, metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "10.42.0.0/32", p.Spec.Ingress[len(p.Spec.Ingress)-1].From[0].IPBlock.CIDR)
	_, err = policies("later").Get(ctx, appk8s.IsolationPolicyName, metav1.GetOptions{})
	assert.Error(t, err, "a project with no namespace yet gets its policy when the namespace is made")
	_, err = policies("blog").Get(ctx, appk8s.IsolationPolicyName, metav1.GetOptions{})
	assert.Error(t, err, "another organization's projects stay as they are")

	shop := db.Project{}
	require.NoError(t, gdb.First(&shop, "slug = ?", "shop").Error)
	require.NoError(t, isolation.SetProjectOpen(ctx, shop.ID, true))
	_, err = policies("shop").Get(ctx, appk8s.IsolationPolicyName, metav1.GetOptions{})
	assert.Error(t, err, "a project opened as an exception has no policy")
	require.NoError(t, isolation.SetProjectOpen(ctx, shop.ID, false))
	_, err = policies("shop").Get(ctx, appk8s.IsolationPolicyName, metav1.GetOptions{})
	assert.NoError(t, err, "put back, it is isolated again")

	require.NoError(t, isolation.Set(ctx, acme.ID, false))
	left, _ = policies("").List(ctx, metav1.ListOptions{})
	assert.Empty(t, left.Items, "switched off, nothing is left behind")
	var saved db.Organization
	require.NoError(t, gdb.First(&saved, "id = ?", acme.ID).Error)
	assert.False(t, saved.NetworkIsolation)
}

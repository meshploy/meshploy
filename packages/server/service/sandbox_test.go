package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
	appk8s "github.com/meshploy/packages/server/k8s"
	"github.com/meshploy/packages/server/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	nodev1 "k8s.io/api/node/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// Switching a project on moves its running apps onto gVisor and its next pods
// with them, and leaves its databases on the host; switching off moves the
// apps back. It is refused while no node can run them, or while an app is
// pinned to a node that cannot; a pinned database does not count.
func TestSandboxingAProject(t *testing.T) {
	gdb := newTestDB(t)
	ctx := context.Background()
	org := db.Organization{Name: "acme", Slug: "acme-" + uuid.NewString()[:8]}
	require.NoError(t, gdb.Create(&org).Error)
	shop := db.Project{OrganizationID: org.ID, Name: "Shop", Slug: "shop"}
	require.NoError(t, gdb.Create(&shop).Error)
	web := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "shop", Labels: map[string]string{"managed-by": "meshploy"}}}
	pg := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "pg-x1y2z3", Namespace: "shop", Labels: map[string]string{"managed-by": "meshploy"}}}
	gvisor := &nodev1.RuntimeClass{ObjectMeta: metav1.ObjectMeta{Name: appk8s.SandboxRuntimeClass}, Handler: "runsc"}

	bare := service.NewSandboxForTest(gdb, fake.NewSimpleClientset(web, gvisor))
	err := bare.Set(ctx, shop.ID, true)
	assert.ErrorIs(t, err, appk8s.ErrNoSandboxNode, "no node is prepared")
	require.NoError(t, gdb.First(&shop, "id = ?", shop.ID).Error)
	assert.False(t, shop.Sandboxed, "a refused switch changes nothing")

	client := fake.NewSimpleClientset(web, pg, gvisor,
		&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "gw", Labels: map[string]string{appk8s.SandboxNodeLabel: "true"}}},
		&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "w1"}})
	sandbox := service.NewSandboxForTest(gdb, client)

	plain := db.Node{OrganizationID: org.ID, Name: "w1", TailscaleIP: "100.64.0.9"}
	require.NoError(t, gdb.Create(&plain).Error)
	pinned := db.Service{ProjectID: shop.ID, Name: "worker", Slug: "worker", NodeID: &plain.ID}
	require.NoError(t, gdb.Create(&pinned).Error)
	assert.ErrorIs(t, sandbox.Set(ctx, shop.ID, true), appk8s.ErrNoSandboxNode, "a service is pinned to a node that cannot")
	require.NoError(t, gdb.Model(&pinned).Update("node_id", nil).Error)
	database := db.Service{ProjectID: shop.ID, Name: "pg", Slug: "pg", Type: db.ServiceTypeDatabase, NodeID: &plain.ID}
	require.NoError(t, gdb.Create(&database).Error)
	require.NoError(t, gdb.Create(&db.DatabaseConfig{ServiceID: database.ID, Engine: "postgres", Version: "17", Slug: "pg-x1y2z3"}).Error)

	require.NoError(t, sandbox.Set(ctx, shop.ID, true))
	d, err := client.AppsV1().Deployments("shop").Get(ctx, "web", metav1.GetOptions{})
	require.NoError(t, err)
	require.NotNil(t, d.Spec.Template.Spec.RuntimeClassName, "the running service moves at once")
	assert.Equal(t, appk8s.SandboxRuntimeClass, *d.Spec.Template.Spec.RuntimeClassName)
	d, err = client.AppsV1().Deployments("shop").Get(ctx, "pg-x1y2z3", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Nil(t, d.Spec.Template.Spec.RuntimeClassName, "the database stays on the host")
	runtime, err := sandbox.RuntimeFor(ctx, shop.ID, "")
	require.NoError(t, err)
	assert.Equal(t, appk8s.SandboxRuntimeClass, runtime, "and its next deploy follows")

	require.NoError(t, sandbox.Set(ctx, shop.ID, false))
	d, _ = client.AppsV1().Deployments("shop").Get(ctx, "web", metav1.GetOptions{})
	assert.Nil(t, d.Spec.Template.Spec.RuntimeClassName)
	runtime, err = sandbox.RuntimeFor(ctx, shop.ID, "")
	require.NoError(t, err)
	assert.Empty(t, runtime)
}

// A rule that requires a project sandboxed wins over its switch: the project
// runs sandboxed, says why, and cannot be switched off.
func TestARequiredSandboxCannotBeSwitchedOff(t *testing.T) {
	gdb := newTestDB(t)
	ctx := context.Background()
	org := db.Organization{Name: "acme", Slug: "acme-" + uuid.NewString()[:8]}
	require.NoError(t, gdb.Create(&org).Error)
	shop := db.Project{OrganizationID: org.ID, Name: "Shop", Slug: "shop"}
	require.NoError(t, gdb.Create(&shop).Error)
	service.UseSandboxRuleForTest(t, func(_ context.Context, p *db.Project) (bool, string, error) {
		return p.ID == shop.ID, "every project here runs sandboxed", nil
	})
	sandbox := service.NewSandboxForTest(gdb, nil)

	state, err := sandbox.State(ctx, &shop)
	require.NoError(t, err)
	assert.Equal(t, service.SandboxState{On: true, Required: true, Why: "every project here runs sandboxed"}, state)
	err = sandbox.Set(ctx, shop.ID, false)
	assert.True(t, errors.Is(err, service.ErrSandboxRequired), "got %v", err)
	assert.Contains(t, err.Error(), "every project here runs sandboxed")
}

// Settings list each sandboxed project and level under its project, those a
// rule requires among them, and the nodes that can run them.
func TestTheSandboxOverview(t *testing.T) {
	gdb := newTestDB(t)
	ctx := context.Background()
	org := db.Organization{Name: "acme", Slug: "acme-" + uuid.NewString()[:8]}
	require.NoError(t, gdb.Create(&org).Error)
	shop := db.Project{OrganizationID: org.ID, Name: "Shop", Slug: "shop-" + uuid.NewString()[:8]}
	blog := db.Project{OrganizationID: org.ID, Name: "Blog", Slug: "blog-" + uuid.NewString()[:8]}
	require.NoError(t, gdb.Create(&shop).Error)
	require.NoError(t, gdb.Create(&blog).Error)
	staging := db.Project{OrganizationID: org.ID, Name: "Shop", Slug: shop.Slug + "-staging",
		ParentProjectID: &shop.ID, EnvName: "staging", EnvLevel: 1, Sandboxed: true}
	require.NoError(t, gdb.Create(&staging).Error)
	service.UseSandboxRuleForTest(t, func(_ context.Context, p *db.Project) (bool, string, error) {
		return p.ID == blog.ID, "required", nil
	})

	client := fake.NewSimpleClientset(
		&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "gw", Labels: map[string]string{appk8s.SandboxNodeLabel: "true"}}},
		&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "w1"}})
	got, err := service.NewSandboxForTest(gdb, client).Overview(ctx, org.ID)
	require.NoError(t, err)
	assert.Equal(t, []string{"gw"}, got.Nodes)
	assert.ElementsMatch(t, []service.SandboxedProject{
		{ID: blog.ID, ProjectID: blog.ID, Name: "Blog", Required: true},
		{ID: staging.ID, ProjectID: shop.ID, Name: "Shop", Level: "staging"},
	}, got.Projects)
}

// A new project starts sandboxed as a rule requires, else as its creator
// asks, else as the organization's setting says; asking for no sandbox where
// one is required, or for one no node can run, is refused.
func TestANewProjectsSandbox(t *testing.T) {
	gdb := newTestDB(t)
	ctx := context.Background()
	org := db.Organization{Name: "acme", Slug: "acme-" + uuid.NewString()[:8]}
	require.NoError(t, gdb.Create(&org).Error)
	gvisor := &nodev1.RuntimeClass{ObjectMeta: metav1.ObjectMeta{Name: appk8s.SandboxRuntimeClass}, Handler: "runsc"}
	ready := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "gw", Labels: map[string]string{appk8s.SandboxNodeLabel: "true"}}}
	sandbox := service.NewSandboxForTest(gdb, fake.NewSimpleClientset(gvisor, ready))
	yes, no := true, false

	on, err := sandbox.NewProject(ctx, org.ID, nil)
	require.NoError(t, err)
	assert.False(t, on, "off by default")

	require.NoError(t, sandbox.SetNewProjects(ctx, org.ID, true))
	on, err = sandbox.NewProject(ctx, org.ID, nil)
	require.NoError(t, err)
	assert.True(t, on, "the organization's setting applies when the creator says nothing")
	on, err = sandbox.NewProject(ctx, org.ID, &no)
	require.NoError(t, err)
	assert.False(t, on, "the creator's choice wins over the setting")

	overview, err := sandbox.Overview(ctx, org.ID)
	require.NoError(t, err)
	assert.True(t, overview.NewProjects)
	assert.False(t, overview.NewProjectsRequired)

	service.UseSandboxRuleForTest(t, func(_ context.Context, p *db.Project) (bool, string, error) {
		return p.OrganizationID == org.ID, "every project in the pool runs sandboxed", nil
	})
	_, err = sandbox.NewProject(ctx, org.ID, &no)
	assert.ErrorIs(t, err, service.ErrSandboxRequired, "a rule outranks the creator")
	on, err = sandbox.NewProject(ctx, org.ID, &yes)
	require.NoError(t, err)
	assert.True(t, on)
	overview, err = sandbox.Overview(ctx, org.ID)
	require.NoError(t, err)
	assert.True(t, overview.NewProjectsRequired)
	assert.Equal(t, "every project in the pool runs sandboxed", overview.NewProjectsWhy)

	bare := service.NewSandboxForTest(gdb, fake.NewSimpleClientset(gvisor))
	_, err = bare.NewProject(ctx, org.ID, &yes)
	assert.ErrorIs(t, err, appk8s.ErrNoSandboxNode, "no node can run it")
	assert.ErrorIs(t, bare.SetNewProjects(ctx, org.ID, true), appk8s.ErrNoSandboxNode)
}

package service_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	meshdb "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The build log of docai-backend, as far as hints go: Railpack found Python,
// no start command, and a FastAPI app loading torch; the repository has a
// Dockerfile that knows how to start it.
const docaiBuildLog = "\x1b[1;34m[meshploy-build]\x1b[0m Stack: language python\n" +
	"\x1b[1;34m[meshploy-build]\x1b[0m Stack: entry fastapi app.main:app\n" +
	"\x1b[1;34m[meshploy-build]\x1b[0m Stack: heavy sentence-transformers torch\n" +
	"\x1b[1;34m[meshploy-build]\x1b[0m Stack: dockerfile Dockerfile cmd\n" +
	"\x1b[1;34m[meshploy-build]\x1b[0m Stack: expose 8080\n" +
	"  ↳ Detected Python\n  No start command detected\n"

func hintByKind(hs []service.Hint, kind string) *service.Hint {
	for i := range hs {
		if hs[i].Kind == kind {
			return &hs[i]
		}
	}
	return nil
}

// A build that finds no start command, and dependencies that need more
// memory than the service has, leaves advice with the fix on the service,
// named on the overview. It goes away when the configuration is fixed, or
// for everyone when dismissed.
func TestABuildLeavesAdviceWithItsFix(t *testing.T) {
	ctx := context.Background()
	gdb := newTestDB(t)
	svcs := newServices(gdb)
	orgID := seedOrg(t, gdb, "acme", nil)
	project, err := svcs.Projects.Create(ctx, orgID, "DocAI", "docai")
	require.NoError(t, err)

	backend := meshdb.Service{ProjectID: project.ID, Name: "docai-backend", Slug: "docai-backend",
		Type: meshdb.ServiceTypeApplication, Status: meshdb.ServiceRunning, MemoryLimit: "512Mi",
		Ports: []meshdb.ServicePort{{Name: "http", Port: 8016, IsHTTP: true, IsPrimary: true}}}
	require.NoError(t, gdb.Create(&backend).Error)
	require.NoError(t, gdb.Create(&meshdb.BuildConfig{ServiceID: backend.ID, GitRepo: "acme/docai", Branch: "main", Builder: meshdb.BuilderRailpack}).Error)
	dep := meshdb.Deployment{ServiceID: backend.ID, Status: meshdb.DeploymentSuccess, Source: meshdb.DeploySourceBuild}
	require.NoError(t, gdb.Create(&dep).Error)
	service.RecordStackForTest(svcs, dep.ID, docaiBuildLog)

	hints, err := svcs.Workloads.Hints(ctx, project.ID)
	require.NoError(t, err)
	hs := hints[backend.ID]
	start := hintByKind(hs, service.HintStartCommand)
	require.NotNil(t, start)
	assert.Equal(t, &dep.ID, start.DeploymentID)
	assert.Contains(t, start.Detail, "FastAPI app in app/main.py")
	require.Len(t, start.Fixes, 2)
	assert.Equal(t, service.HintFix{Action: service.HintFixStartCommand, Label: "Set start command",
		Value: "uvicorn app.main:app --host 0.0.0.0 --port $PORT"}, start.Fixes[0])
	assert.Equal(t, "dockerfile", start.Fixes[1].Value)
	mem := hintByKind(hs, service.HintMemory)
	require.NotNil(t, mem)
	assert.Equal(t, "2Gi", mem.Fixes[0].Value)
	assert.Contains(t, mem.Detail, "sentence-transformers and torch")
	assert.Nil(t, hintByKind(hs, service.HintPort), "EXPOSE says nothing about a Railpack build")

	overview, err := svcs.Overview.Get(ctx, orgID, []uuid.UUID{project.ID}, true)
	require.NoError(t, err)
	var item *service.AttentionItem
	for i := range overview.Attention {
		if overview.Attention[i].Kind == "service_hints" {
			item = &overview.Attention[i]
		}
	}
	require.NotNil(t, item)
	assert.Equal(t, "docai-backend: no start command and likely to need more memory", item.Title)
	assert.Equal(t, backend.ID, *item.ServiceID)

	// A start command written with the wrong port: the start command hint
	// goes, a port one comes.
	require.NoError(t, gdb.Model(&backend).Update("start_command", "uvicorn app.main:app --host 0.0.0.0 --port 8080").Error)
	hints, err = svcs.Workloads.Hints(ctx, project.ID)
	require.NoError(t, err)
	assert.Nil(t, hintByKind(hints[backend.ID], service.HintStartCommand))
	port := hintByKind(hints[backend.ID], service.HintPort)
	require.NotNil(t, port)
	assert.Equal(t, "8080", port.Fixes[0].Value)
	assert.Equal(t, "uvicorn app.main:app --host 0.0.0.0 --port $PORT", port.Fixes[1].Value)

	// Enough memory, and the port dismissed: nothing left to say.
	require.NoError(t, gdb.Model(&backend).Update("memory_limit", "3Gi").Error)
	require.NoError(t, svcs.Workloads.DismissHint(ctx, backend.ID, service.HintPort))
	require.NoError(t, svcs.Workloads.DismissHint(ctx, backend.ID, service.HintPort))
	hints, err = svcs.Workloads.Hints(ctx, project.ID)
	require.NoError(t, err)
	assert.Empty(t, hints[backend.ID])
	assert.Error(t, svcs.Workloads.DismissHint(ctx, backend.ID, "nonsense"))
}

// Building with the Dockerfile, the port it EXPOSEs is set against the
// service's.
func TestADockerfileThatListensElsewhereSaysSo(t *testing.T) {
	ctx := context.Background()
	gdb := newTestDB(t)
	svcs := newServices(gdb)
	orgID := seedOrg(t, gdb, "acme", nil)
	project, err := svcs.Projects.Create(ctx, orgID, "Web", "web")
	require.NoError(t, err)
	web := meshdb.Service{ProjectID: project.ID, Name: "web", Slug: "web", Type: meshdb.ServiceTypeApplication, MemoryLimit: "512Mi",
		Ports: []meshdb.ServicePort{{Name: "http", Port: 3000, IsHTTP: true, IsPrimary: true}}}
	require.NoError(t, gdb.Create(&web).Error)
	require.NoError(t, gdb.Create(&meshdb.BuildConfig{ServiceID: web.ID, GitRepo: "acme/web", Branch: "main", Builder: meshdb.BuilderDockerfile}).Error)
	dep := meshdb.Deployment{ServiceID: web.ID, Status: meshdb.DeploymentSuccess, Source: meshdb.DeploySourceBuild}
	require.NoError(t, gdb.Create(&dep).Error)
	service.RecordStackForTest(svcs, dep.ID, "Stack: language node\nStack: dockerfile Dockerfile cmd\nStack: expose 8080\n")

	hints, err := svcs.Workloads.Hints(ctx, project.ID)
	require.NoError(t, err)
	require.Len(t, hints[web.ID], 1)
	assert.Equal(t, service.HintPort, hints[web.ID][0].Kind)
	assert.Equal(t, "8080", hints[web.ID][0].Fixes[0].Value)
}

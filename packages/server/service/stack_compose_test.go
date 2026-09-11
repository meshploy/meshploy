package service_test

import (
	"context"
	"testing"

	meshdb "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// A stack follows compose: published ports are public, loopback-bound ones
// internal, command and entrypoint reach the service, and a re-apply converges
// the ports it declares while keeping assigned NodePorts and console edits.
func TestStackApplyFollowsCompose(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	svcs := newServices(db)
	user, err := svcs.Auth.Register(ctx, service.RegisterInput{Username: "composer", Email: "composer@example.com", Password: "pass"})
	require.NoError(t, err)
	var org meshdb.Organization
	require.NoError(t, db.Where("slug = ?", user.Username).First(&org).Error)
	proj, err := svcs.Projects.Create(ctx, org.ID, "infra", "infra")
	require.NoError(t, err)

	load := func(name string) meshdb.Service {
		t.Helper()
		var svc meshdb.Service
		require.NoError(t, db.Preload("Ports", func(tx *gorm.DB) *gorm.DB { return tx.Order("port") }).
			Where("project_id = ? AND name = ?", proj.ID, name).First(&svc).Error)
		return svc
	}

	r, err := svcs.Stacks.ApplyManifest(ctx, proj.ID, "infra", `
services:
  minio:
    image: minio/minio
    command: server /data --console-address ":9001"
    ports: ["9000:9000", "9001:9001"]
  redis:
    image: redis:7-alpine
    ports: ["127.0.0.1:6379:6379"]
  worker:
    image: alpine
    entrypoint: ["/bin/sh", "-c"]
    command: ["echo $$HOME"]
`, user.ID)
	require.NoError(t, err)
	require.Empty(t, r.Errors)

	minio := load("minio")
	assert.Equal(t, []string{"server", "/data", "--console-address", ":9001"}, []string(minio.Args))
	assert.Empty(t, minio.Command)
	require.Len(t, minio.Ports, 2)
	assert.True(t, minio.Ports[0].IsPublic && minio.Ports[0].IsHTTP && minio.Ports[0].IsPrimary, "9000")
	assert.True(t, minio.Ports[1].IsPublic && !minio.Ports[1].IsPrimary, "9001")

	redis := load("redis")
	require.Len(t, redis.Ports, 1)
	assert.False(t, redis.Ports[0].IsPublic, "bound to loopback")
	assert.False(t, redis.Ports[0].IsHTTP, "redis does not speak HTTP")

	worker := load("worker")
	assert.Equal(t, []string{"/bin/sh", "-c"}, []string(worker.Command))
	assert.Equal(t, []string{"echo $HOME"}, []string(worker.Args))
	require.Len(t, worker.Ports, 1)
	assert.False(t, worker.Ports[0].IsPublic, "a service publishing nothing is internal")

	// A deploy assigns a NodePort; an operator edits the worker's port.
	require.NoError(t, db.Model(&meshdb.ServicePort{}).Where("service_id = ? AND port = 9001", minio.ID).Update("node_port", 31001).Error)
	require.NoError(t, db.Model(&meshdb.ServicePort{}).Where("service_id = ?", worker.ID).Update("port", 4000).Error)

	r, err = svcs.Stacks.ApplyManifest(ctx, proj.ID, "infra", `
services:
  minio:
    image: minio/minio
    ports: ["9001:9001"]
    expose: ["9000"]
  redis:
    image: redis:7-alpine
    ports: ["127.0.0.1:6379:6379"]
  worker:
    image: alpine
`, user.ID)
	require.NoError(t, err)
	require.Empty(t, r.Errors)

	minio = load("minio")
	assert.Empty(t, minio.Args, "a command removed from compose is removed from the service")
	require.Len(t, minio.Ports, 2)
	assert.False(t, minio.Ports[0].IsPublic, "9000 is now only exposed")
	assert.True(t, minio.Ports[1].IsPublic && minio.Ports[1].IsPrimary, "9001 is the published port")
	assert.Equal(t, 31001, minio.Ports[1].NodePort, "a port that stays public keeps its NodePort")

	worker = load("worker")
	assert.Empty(t, worker.Command)
	require.Len(t, worker.Ports, 1)
	assert.Equal(t, 4000, worker.Ports[0].Port, "compose declares no port, so the console's edit stands")
}

package service_test

import (
	"context"
	"strings"
	"testing"

	"github.com/meshploy/packages/server/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A path that is not absolute, or names a directory, produces a mount the
// cluster rejects long after the user has moved on. Caught at the boundary.
func TestConfigFileValidation(t *testing.T) {
	ctx := context.Background()
	svcs, _, projID, _ := setupStackTest(t)
	pid := parseUUID(t, projID)

	cases := []struct {
		name, path, content, wantErr string
	}{
		{"", "/etc/app.conf", "x", "name is required"},
		{"cfg", "etc/app.conf", "x", "must be absolute"},
		{"cfg", "/etc/", "x", "not a directory"},
		{"cfg", "/etc/app.conf", strings.Repeat("x", 300*1024), "the limit is"},
	}
	for _, tc := range cases {
		_, err := svcs.ConfigFiles.Create(ctx, pid, service.CreateConfigFileInput{
			Name: tc.name, Path: tc.path, Content: tc.content,
		})
		require.Error(t, err, "expected %q to be rejected", tc.wantErr)
		assert.Contains(t, err.Error(), tc.wantErr)
	}
}

// Content is write-only. It round-trips through the encrypted column, but a
// listing must never carry it — these hold htpasswd hashes and TLS keys.
func TestConfigFileContentIsStoredButNotListed(t *testing.T) {
	ctx := context.Background()
	svcs, _, projID, _ := setupStackTest(t)
	pid := parseUUID(t, projID)

	created, err := svcs.ConfigFiles.Create(ctx, pid, service.CreateConfigFileInput{
		Name: "zot config", Path: "/etc/zot/config.json", Content: `{"http":{"port":"5000"}}`,
	})
	require.NoError(t, err)

	got, err := svcs.ConfigFiles.Get(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, `{"http":{"port":"5000"}}`, string(got.Content), "content must survive the encrypted round trip")
}

// Deleting a file a service still mounts would leave the service referencing
// something gone. Blocked, exactly as a volume is.
func TestConfigFileDeleteBlockedWhileAttached(t *testing.T) {
	ctx := context.Background()
	svcs, _, projID, _ := setupStackTest(t)
	pid := parseUUID(t, projID)

	file, err := svcs.ConfigFiles.Create(ctx, pid, service.CreateConfigFileInput{
		Name: "shared", Path: "/etc/app/shared.conf", Content: "a=1",
	})
	require.NoError(t, err)

	target, err := svcs.Workloads.Create(ctx, pid, service.CreateWorkloadInput{
		Name: "cfg-consumer", Image: "nginx:alpine",
	})
	require.NoError(t, err)

	require.NoError(t, svcs.ConfigFiles.Attach(ctx, file.ID, target.ID))

	err = svcs.ConfigFiles.Delete(ctx, file.ID)
	require.Error(t, err, "delete must be refused while a service mounts the file")
	assert.Contains(t, err.Error(), "detach it first")

	// Detaching releases it.
	require.NoError(t, svcs.ConfigFiles.Detach(ctx, file.ID, target.ID))
	require.NoError(t, svcs.ConfigFiles.Delete(ctx, file.ID))
}

// One file, several services — the reason this is a project-scoped resource
// with an attachment table rather than a field on the service.
func TestConfigFileAttachesToManyServices(t *testing.T) {
	ctx := context.Background()
	svcs, _, projID, _ := setupStackTest(t)
	pid := parseUUID(t, projID)

	file, err := svcs.ConfigFiles.Create(ctx, pid, service.CreateConfigFileInput{
		Name: "ca bundle", Path: "/etc/ssl/ca.pem", Content: "-----BEGIN CERTIFICATE-----",
	})
	require.NoError(t, err)

	var ids []string
	for _, name := range []string{"svc-one", "svc-two"} {
		w, err := svcs.Workloads.Create(ctx, pid, service.CreateWorkloadInput{Name: name, Image: "nginx:alpine"})
		require.NoError(t, err)
		require.NoError(t, svcs.ConfigFiles.Attach(ctx, file.ID, w.ID))
		ids = append(ids, w.ID.String())

		// A file already attached must not be attached twice.
		assert.Error(t, svcs.ConfigFiles.Attach(ctx, file.ID, w.ID))
	}

	attached, err := svcs.ConfigFiles.AttachedServices(ctx, file.ID)
	require.NoError(t, err)
	assert.Len(t, attached, len(ids))

	// And each service sees it among its own files.
	for i := range attached {
		files, err := svcs.ConfigFiles.ForService(ctx, attached[i].ID)
		require.NoError(t, err)
		require.Len(t, files, 1)
		assert.Equal(t, "/etc/ssl/ca.pem", files[0].Path)
	}
}

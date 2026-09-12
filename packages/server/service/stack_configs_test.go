package service_test

import (
	"context"
	"strings"
	"testing"

	meshdb "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const configsSpec = `
services:
  keycloak:
    image: quay.io/keycloak/keycloak:26.0
    configs:
      - source: realm
        target: /opt/keycloak/data/import/realm.json
      - healthcheck
    secrets:
      - api_secret
    volumes:
      - ./local.conf:/etc/local.conf
    extra_hosts:
      - "host.docker.internal:host-gateway"
    environment:
      DB_HOST: host.docker.internal
configs:
  realm:
    file: ./keycloak/realm.json
  healthcheck:
    content: |
      #!/bin/bash
      exit 0
secrets:
  api_secret:
    file: ./secrets/api.txt
`

// Compose configs and secrets become the service's config files, from
// content: or from the files sent with the manifest; what a stack cannot carry
// is named in the warnings; and an apply without the files keeps the copies.
func TestStackApplyCarriesComposeConfigs(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	svcs := newServices(db)
	user, err := svcs.Auth.Register(ctx, service.RegisterInput{Username: "configs", Email: "configs@example.com", Password: "pass"})
	require.NoError(t, err)
	var org meshdb.Organization
	require.NoError(t, db.Where("slug = ?", user.Username).First(&org).Error)
	proj, err := svcs.Projects.Create(ctx, org.ID, "auth", "auth")
	require.NoError(t, err)

	files := map[string]string{
		"./keycloak/realm.json": `{"realm":"procureflow"}`,
		"secrets/api.txt":       "s3cret",
	}
	r, err := svcs.Stacks.ApplyManifest(ctx, proj.ID, "auth", configsSpec, user.ID, files)
	require.NoError(t, err)
	require.Empty(t, r.Errors)

	stored := func() map[string]string {
		var rows []meshdb.ConfigFile
		require.NoError(t, db.Where("project_id = ?", proj.ID).Find(&rows).Error)
		m := map[string]string{}
		for _, f := range rows {
			m[f.Path] = string(f.Content)
		}
		return m
	}
	assert.Equal(t, map[string]string{
		"/opt/keycloak/data/import/realm.json": `{"realm":"procureflow"}`,
		"/healthcheck":                         "#!/bin/bash\nexit 0\n",
		"/run/secrets/api_secret":              "s3cret",
	}, stored())

	var kc meshdb.Service
	require.NoError(t, db.Where("project_id = ? AND name = ?", proj.ID, "keycloak").First(&kc).Error)
	var attached int64
	require.NoError(t, db.Model(&meshdb.ServiceConfigFile{}).Where("service_id = ?", kc.ID).Count(&attached).Error)
	assert.EqualValues(t, 3, attached)

	warnings := strings.Join(r.Warnings, "\n")
	assert.Contains(t, warnings, "bind mount at /etc/local.conf left out")
	assert.Contains(t, warnings, "extra_hosts left out")
	assert.Contains(t, warnings, "DB_HOST points at host.docker.internal")

	// Applied again without the files, as the console would: the stored
	// copies stay, and the apply says so.
	r, err = svcs.Stacks.ApplyManifest(ctx, proj.ID, "auth", configsSpec, user.ID, nil)
	require.NoError(t, err)
	require.Empty(t, r.Errors)
	assert.Equal(t, `{"realm":"procureflow"}`, stored()["/opt/keycloak/data/import/realm.json"])
	assert.Contains(t, strings.Join(r.Warnings, "\n"), `config "realm" kept the copy an earlier apply stored`)

	// A file no apply has read is left out, with a way to supply it.
	r, err = svcs.Stacks.ApplyManifest(ctx, proj.ID, "other", `
services:
  web:
    image: nginx
    configs: [site]
configs:
  site:
    file: ./site.conf
`, user.ID, nil)
	require.NoError(t, err)
	assert.Contains(t, strings.Join(r.Warnings, "\n"), `config "site" left out`)
}

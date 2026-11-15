package service

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/meshploy/packages/server/templates"
)

// A template's exposed services are written into its file as routes the
// stack's apply reads, and nothing else in the file changes.
func TestDeclareExposedRoutes(t *testing.T) {
	spec := `# umami, from the catalog
services:
  umami:
    image: ghcr.io/umami-software/umami:postgresql-v2.19.0   # pinned
    environment:
      DATABASE_URL: postgres://umami:${DB_PASSWORD}@umami-db:5432/umami
  umami-db:
    image: postgres:16
    x-meshploy:
      type: database
  admin:
    image: adminer:5
    x-meshploy:
      deploy:
        port: 8080
  docs:
    image: nginx:1.27
    x-meshploy:
      route:
        subdomain: kept
`
	out, err := declareExposedRoutes(spec, []templates.ResolvedExpose{
		{Service: "umami", Port: 3000, Subdomain: "umami-d43319"},
		{Service: "admin", Port: 8080, Subdomain: "admin-1"},
		{Service: "admin", Port: 8081, Subdomain: "admin-2"},
		{Service: "docs", Port: 80, Subdomain: "replaced"},
	})
	require.NoError(t, err)
	assert.Contains(t, out, "# umami, from the catalog")
	assert.Contains(t, out, "# pinned")
	assert.Contains(t, out, "${DB_PASSWORD}")

	var doc struct {
		Services map[string]struct {
			XMeshploy map[string]any `yaml:"x-meshploy"`
		} `yaml:"services"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(out), &doc))
	routes := func(name string) meshployRoutes {
		t.Helper()
		b, err := json.Marshal(doc.Services[name].XMeshploy["route"])
		require.NoError(t, err)
		var r meshployRoutes
		require.NoError(t, json.Unmarshal(b, &r))
		return r
	}
	assert.Equal(t, meshployRoutes{{Subdomain: "umami-d43319", Port: 3000}}, routes("umami"))
	assert.Equal(t, meshployRoutes{{Subdomain: "admin-1", Port: 8080}, {Subdomain: "admin-2", Port: 8081}}, routes("admin"))
	assert.Equal(t, meshployRoutes{{Subdomain: "kept"}}, routes("docs"), "a route already declared is the file's own")
	assert.Equal(t, 8080, doc.Services["admin"].XMeshploy["deploy"].(map[string]any)["port"])
	assert.Nil(t, doc.Services["umami-db"].XMeshploy["route"])
}

func TestDeclareExposedRoutesMissingService(t *testing.T) {
	_, err := declareExposedRoutes("services:\n  web:\n    image: nginx\n",
		[]templates.ResolvedExpose{{Service: "api", Port: 80, Subdomain: "api"}})
	assert.ErrorContains(t, err, `"api" is not in the compose file`)
}

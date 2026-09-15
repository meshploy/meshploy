package client_test

import (
	"net/http"
	"testing"

	"github.com/meshploy/packages/client"
)

func TestListTemplates(t *testing.T) {
	srv := newServer(t, []routeHandler{
		{
			method: "GET",
			path:   "/api/v1/templates",
			fn: func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, []client.Template{
					{ID: "zot", Name: "Zot", Category: "registry", Variables: []client.TemplateVariable{
						{Key: "ADMIN_PASSWORD", Generate: "password"},
					}},
				})
			},
		},
	})
	defer srv.Close()

	c := client.New(srv.URL, "token")
	tpls, err := c.ListTemplates()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tpls) != 1 || tpls[0].ID != "zot" {
		t.Fatalf("want the zot template, got %+v", tpls)
	}
	if len(tpls[0].Variables) != 1 || tpls[0].Variables[0].Generate != "password" {
		t.Errorf("variable declarations did not survive: %+v", tpls[0].Variables)
	}
}

func TestGetTemplate(t *testing.T) {
	srv := newServer(t, []routeHandler{
		{
			method: "GET",
			path:   "/api/v1/templates/zot",
			fn: func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, client.TemplateDetail{
					Manifest: &client.Template{ID: "zot", Name: "Zot"},
					Compose:  "services:\n  zot:\n    image: ghcr.io/project-zot/zot:latest\n",
				})
			},
		},
	})
	defer srv.Close()

	c := client.New(srv.URL, "token")
	detail, err := c.GetTemplate("zot")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if detail.Manifest == nil || detail.Manifest.ID != "zot" {
		t.Errorf("want the zot manifest, got %+v", detail.Manifest)
	}
	if detail.Compose == "" {
		t.Error("a template detail without its compose cannot be reviewed before deploying")
	}
}

func TestDeployTemplate(t *testing.T) {
	srv := newServer(t, []routeHandler{
		{
			method: "POST",
			path:   "/api/v1/orgs/" + testOrg + "/projects/" + testProject + "/templates/zot/deploy",
			fn: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusCreated)
				writeJSON(w, client.Stack{ID: "st1", Name: "zot", Status: "deploying"})
			},
		},
	})
	defer srv.Close()

	c := client.New(srv.URL, "token")
	st, err := c.DeployTemplate(testOrg, testProject, "zot", client.DeployTemplateBody{
		PromptValues: map[string]string{"ADMIN_USER": "pritthish"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if st.Name != "zot" {
		t.Errorf("want stack %q, got %q", "zot", st.Name)
	}
}

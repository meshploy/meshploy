package handler

import (
	"reflect"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	svc "github.com/meshploy/packages/server/service"
)

// schemaProperties resolves a type through Huma's own reflection — the same
// path that produces the response schema — and returns its property names.
func schemaProperties(t *testing.T, v any, name string) map[string]*huma.Schema {
	t.Helper()
	reg := huma.NewMapRegistry("#/components/schemas/", huma.DefaultSchemaNamer)
	s := reg.Schema(reflect.TypeOf(v), true, name)
	if s.Ref != "" {
		s = reg.SchemaFromRef(s.Ref)
	}
	if s == nil || s.Properties == nil {
		t.Fatalf("%s produced no properties at all", name)
	}
	return s.Properties
}

// Huma's getFields drops any struct field failing IsExported() *before* it
// checks whether the field is anonymous, so an embedded unexported type is
// skipped whole. encoding/json promotes such fields, Huma does not — so a DTO
// that marshals perfectly in a unit test can still reach the browser missing
// most of itself, with no error anywhere.
//
// That is exactly what happened here: the config file detail endpoint returned
// only attached_services and updated_at, and the detail page rendered a file
// with no name, path or size. This asserts the wire contract instead of
// trusting the Go type to describe it.
func TestConfigFileDetailSchemaCarriesEveryField(t *testing.T) {
	props := schemaProperties(t, configFileDetailDTO{}, "ConfigFileDetail")

	for _, want := range []string{
		"id", "name", "path", "stack_id", "size", "services", "created_at",
		"attached_services", "updated_at",
	} {
		if _, ok := props[want]; !ok {
			t.Errorf("response schema is missing %q — it will be absent from the JSON", want)
		}
	}
}

// The list endpoint's row type is the shape the whole Config tab renders from.
func TestConfigFileListSchemaCarriesEveryField(t *testing.T) {
	props := schemaProperties(t, configFileDTO{}, "ConfigFile")

	for _, want := range []string{"id", "name", "path", "stack_id", "size", "services", "created_at"} {
		if _, ok := props[want]; !ok {
			t.Errorf("response schema is missing %q", want)
		}
	}
}

// ProjectWithCounts embeds two structs, and the project tab bar renders every
// count from them. The embeds are of EXPORTED types, which is the only reason
// Huma promotes their fields at all — so this pins the distinction rather than
// leaving the tab counts resting on it silently.
func TestProjectCountsSchemaCarriesEveryCount(t *testing.T) {
	props := schemaProperties(t, svc.ProjectWithCounts{}, "ProjectWithCounts")

	for _, want := range []string{
		"services_count", "databases_count", "routes_count", "secrets_count",
		"jobs_count", "stacks_count", "volumes_count", "config_files_count",
	} {
		if _, ok := props[want]; !ok {
			t.Errorf("response schema is missing %q — that tab renders no count", want)
		}
	}
	// The project's own fields come from the other embed.
	if _, ok := props["name"]; !ok {
		t.Error("the embedded project fields are missing")
	}
}

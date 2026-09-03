package handler

import (
	"reflect"
	"testing"

	"github.com/danielgtaylor/huma/v2"
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

package mcpserver

import (
	"encoding/json"
	"testing"
)

// structuredContent must be a JSON object. A bare array is what made every
// list_* tool unusable; null and scalars fail the same validation.
func TestJSONResultStructuredContentIsAlwaysAnObject(t *testing.T) {
	type proj struct {
		Name string `json:"name"`
	}
	cases := []struct {
		name      string
		in        any
		wantObj   bool
		wantText  string
		wantCount int
	}{
		{"slice", []proj{{"a"}, {"b"}}, true, `[{"name":"a"},{"name":"b"}]`, 2},
		{"empty slice", []proj{}, true, `[]`, 0},
		{"nil slice", []proj(nil), true, `[]`, 0},
		{"object passes through", proj{"a"}, false, `{"name":"a"}`, 0},
		{"pointer to struct", &proj{"a"}, false, `{"name":"a"}`, 0},
		// A typed nil pointer marshals to null, which is not an object either.
		{"nil pointer", (*proj)(nil), false, `null`, 0},
		{"bare string", "hello", false, `"hello"`, 0},
		{"bare number", 42, false, `42`, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := jsonResult(tc.in)
			if err != nil {
				t.Fatalf("jsonResult: %v", err)
			}
			b, err := json.Marshal(res.StructuredContent)
			if err != nil {
				t.Fatalf("marshal structured: %v", err)
			}
			if got := string(b); (got[0] == '{') != true {
				t.Fatalf("structuredContent must be an object, got %s", got)
			}
			if tc.wantObj {
				var m struct {
					Items json.RawMessage `json:"items"`
					Count int             `json:"count"`
				}
				if err := json.Unmarshal(b, &m); err != nil {
					t.Fatalf("unmarshal wrapper: %v", err)
				}
				if string(m.Items) != tc.wantText {
					t.Errorf("items = %s, want %s", m.Items, tc.wantText)
				}
				if m.Count != tc.wantCount {
					t.Errorf("count = %d, want %d", m.Count, tc.wantCount)
				}
			}
		})
	}
}

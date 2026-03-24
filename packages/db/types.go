package db

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
)

// EnvVarsMap is a JSONB-backed map[string]string for non-sensitive env vars.
type EnvVarsMap map[string]string

func (e EnvVarsMap) Value() (driver.Value, error) { return json.Marshal(e) }
func (e *EnvVarsMap) Scan(value any) error {
	b, ok := value.([]byte)
	if !ok {
		return fmt.Errorf("EnvVarsMap: expected []byte, got %T", value)
	}
	return json.Unmarshal(b, e)
}

// JSONObject is a JSONB-backed map[string]any for flexible structured data
// (node labels, notification configs, template manifests, etc.)
type JSONObject map[string]any

func (j JSONObject) Value() (driver.Value, error) { return json.Marshal(j) }
func (j *JSONObject) Scan(value any) error {
	b, ok := value.([]byte)
	if !ok {
		return fmt.Errorf("JSONObject: expected []byte, got %T", value)
	}
	return json.Unmarshal(b, j)
}

// StringArray is a JSONB-backed []string for notification event lists.
type StringArray []string

func (s StringArray) Value() (driver.Value, error) { return json.Marshal(s) }
func (s *StringArray) Scan(value any) error {
	b, ok := value.([]byte)
	if !ok {
		return fmt.Errorf("StringArray: expected []byte, got %T", value)
	}
	return json.Unmarshal(b, s)
}

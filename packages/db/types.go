package db

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
)

// EnvVarsMap is a JSONB-backed map for service environment variables.
type EnvVarsMap map[string]string

func (e EnvVarsMap) Value() (driver.Value, error) {
	return json.Marshal(e)
}

func (e *EnvVarsMap) Scan(value any) error {
	bytes, ok := value.([]byte)
	if !ok {
		return fmt.Errorf("EnvVarsMap: expected []byte, got %T", value)
	}
	return json.Unmarshal(bytes, e)
}

package client

import (
	"fmt"
	"strings"
)

// ParseKeyValues reads a KEY=VALUE block, the shape every caller that sends a
// set of values uses: the CLI's --env-file, the MCP tools' variables argument.
// Blank lines and # comments are skipped, surrounding quotes are stripped and a
// literal \n becomes a newline, so a block means the same thing here as it does
// in a service's env block.
func ParseKeyValues(text string) (map[string]string, error) {
	out := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.IndexByte(line, '=')
		if idx < 1 {
			return nil, fmt.Errorf("expected KEY=VALUE per line, got %q", line)
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		if len(val) >= 2 && ((val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'')) {
			val = val[1 : len(val)-1]
		}
		out[key] = strings.ReplaceAll(val, `\n`, "\n")
	}
	return out, nil
}

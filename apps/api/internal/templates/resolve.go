package templates

import (
	"fmt"
	"strings"
)

// ResolvedExpose is a web-facing service/port plus the subdomain assigned to it.
// The deploy flow turns each into a Route after the stack is applied.
// Subdomain is the label only (e.g. "pgadmin-ab12ef"); Hostname is the full name
// (e.g. "pgadmin-ab12ef.acme.com") used for ${VAR} substitution.
type ResolvedExpose struct {
	Service   string
	Port      int
	Subdomain string
	Hostname  string
}

// Resolve builds the full variable map for a deploy: it validates prompted
// values (required/present), runs value generators, and assigns a subdomain for
// each subdomain variable. baseDomain is the org's base domain (e.g. "acme.com").
//
// Generated secret values are returned in the map so the caller can persist them
// as EncryptedString columns; they are never logged or echoed.
func Resolve(m *Manifest, promptValues map[string]string, baseDomain string) (vars map[string]string, exposes []ResolvedExpose, err error) {
	vars = make(map[string]string, len(m.Variables))
	for _, v := range m.Variables {
		switch {
		case v.IsPrompt():
			val, ok := promptValues[v.Key]
			if v.Required && (!ok || strings.TrimSpace(val) == "") {
				return nil, nil, fmt.Errorf("required variable %q is missing", v.Key)
			}
			vars[v.Key] = val

		case v.IsSubdomain():
			if baseDomain == "" {
				return nil, nil, fmt.Errorf("variable %q needs a subdomain but the org has no base domain", v.Key)
			}
			label := fmt.Sprintf("%s-%s", m.ID, randHex(3))
			host := label + "." + baseDomain
			vars[v.Key] = host
			if v.Expose != nil {
				exposes = append(exposes, ResolvedExpose{
					Service:   v.Expose.Service,
					Port:      v.Expose.Port,
					Subdomain: label,
					Hostname:  host,
				})
			}

		default: // value generator (password, secret64, hex32, uuid)
			val, gerr := generateValue(v.Generate)
			if gerr != nil {
				return nil, nil, fmt.Errorf("variable %q: %w", v.Key, gerr)
			}
			vars[v.Key] = val
		}
	}
	return vars, exposes, nil
}

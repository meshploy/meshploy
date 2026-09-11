package service

import (
	"regexp"
	"sort"

	corev1 "k8s.io/api/core/v1"
)

// envRef matches $${ (an escaped, literal "${") and ${NAME}.
var envRef = regexp.MustCompile(`\$\$\{|\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// resolveEnvRefs replaces ${NAME} in each value with the value of NAME among
// envs, so an app that reads DATABASE_URL can be given ${PRIMARY_PG_DB_URL}.
//
// One level only: a substituted value is not expanded again, which keeps the
// result predictable and leaves no cycle to detect. $${ is a literal "${". A
// name envs does not define is left as written and returned, sorted, so the
// caller can say so rather than hand the app an empty value.
func resolveEnvRefs(envs []corev1.EnvVar) ([]corev1.EnvVar, []string) {
	values := make(map[string]string, len(envs))
	for _, e := range envs {
		values[e.Name] = e.Value
	}
	seen := map[string]bool{}
	var unresolved []string
	out := make([]corev1.EnvVar, len(envs))
	for i, e := range envs {
		e.Value = envRef.ReplaceAllStringFunc(e.Value, func(m string) string {
			if m == "$${" {
				return "${"
			}
			name := m[2 : len(m)-1]
			if v, ok := values[name]; ok {
				return v
			}
			if !seen[name] {
				seen[name] = true
				unresolved = append(unresolved, name)
			}
			return m
		})
		out[i] = e
	}
	sort.Strings(unresolved)
	return out, unresolved
}

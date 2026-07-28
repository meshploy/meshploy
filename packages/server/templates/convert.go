package templates

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// varRefRe matches a ${VAR} placeholder. Keys are uppercase + digits + underscore.
var varRefRe = regexp.MustCompile(`\$\{([A-Z0-9_]+)\}`)

// References returns the unique ${VAR} keys referenced in a compose spec.
// Used to validate the var↔placeholder mapping.
func References(spec string) []string {
	set := map[string]struct{}{}
	for _, m := range varRefRe.FindAllStringSubmatch(spec, -1) {
		set[m[1]] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Substitute replaces every ${VAR} in the compose spec with its resolved value.
//
// This is the only deploy-time spec conversion: the input is the template's
// compose (with x-meshploy blocks already in place), the output is a valid
// Meshploy stack spec ready for StackService.Create/Apply. It errors if any
// ${VAR} has no resolved value, so unresolved placeholders surface here rather
// than as a confusing compose-loader error later.
func Substitute(spec string, vars map[string]string) (string, error) {
	var missing []string
	out := varRefRe.ReplaceAllStringFunc(spec, func(ref string) string {
		key := varRefRe.FindStringSubmatch(ref)[1]
		val, ok := vars[key]
		if !ok {
			missing = append(missing, key)
			return ref
		}
		return val
	})
	if len(missing) > 0 {
		return "", fmt.Errorf("unresolved template variables: %s", strings.Join(dedupeSorted(missing), ", "))
	}
	return out, nil
}

func dedupeSorted(in []string) []string {
	set := map[string]struct{}{}
	for _, s := range in {
		set[s] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

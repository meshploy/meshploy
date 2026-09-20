package service

import (
	"regexp"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
)

// envRef matches $${ (an escaped, literal "${") and ${NAME}.
var envRef = regexp.MustCompile(`\$\$\{|\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// resolveEnvRefs replaces ${NAME} in each value with the value of NAME among
// envs, so an app that reads DATABASE_URL can be given ${PRIMARY_PG_DB_URL}.
//
// References are followed to a value: with DATABASE_URL=${PRIMARY_PG_DB_URL},
// ${DATABASE_URL} is the URL itself. $${ is a literal "${", and stays literal
// however many references lead to it.
//
// Nothing is guessed. A name envs does not define is left as written and
// returned in unknown; a variable whose references go round in a loop, or lead
// into one, keeps its value as written and is returned in looped. Both are
// sorted, so the caller can say so rather than hand the app a value it did not
// mean.
func resolveEnvRefs(envs []corev1.EnvVar) (out []corev1.EnvVar, unknown, looped []string) {
	r := envResolver{
		raw:      make(map[string]string, len(envs)),
		done:     map[string]string{},
		visiting: map[string]bool{},
		unknown:  map[string]bool{},
	}
	for _, e := range envs {
		r.raw[e.Name] = e.Value
	}
	loops := map[string]bool{}
	out = make([]corev1.EnvVar, len(envs))
	for i, e := range envs {
		r.visiting[e.Name] = true
		v, ok := r.expand(e.Value)
		delete(r.visiting, e.Name)
		if ok {
			e.Value = v
		} else {
			loops[e.Name] = true
		}
		out[i] = e
	}
	return out, sortedRefNames(r.unknown), sortedRefNames(loops)
}

type envResolver struct {
	raw, done         map[string]string
	visiting, unknown map[string]bool
}

// value is name's value with its references followed. ok is false when they
// lead back to a variable still being resolved; the result is then not kept,
// because it depends on where the loop was entered.
func (r *envResolver) value(name string) (string, bool) {
	if v, ok := r.done[name]; ok {
		return v, true
	}
	r.visiting[name] = true
	v, ok := r.expand(r.raw[name])
	delete(r.visiting, name)
	if ok {
		r.done[name] = v
	}
	return v, ok
}

// expand replaces the references in s. A substituted value is already expanded,
// so it is not scanned again, which is what keeps an escaped $${ literal.
func (r *envResolver) expand(s string) (string, bool) {
	ok := true
	out := envRef.ReplaceAllStringFunc(s, func(m string) string {
		if m == "$${" {
			return "${"
		}
		name := m[2 : len(m)-1]
		if _, defined := r.raw[name]; !defined {
			r.unknown[name] = true
			return m
		}
		if r.visiting[name] {
			ok = false
			return m
		}
		v, clean := r.value(name)
		if !clean {
			ok = false
		}
		return v
	})
	return out, ok
}

func sortedRefNames(set map[string]bool) []string {
	names := make([]string, 0, len(set))
	for n := range set {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// ── Build-time references ────────────────────────────────────────────────────

// resolveBuildEnv fills in ${NAME} references in a service's build environment.
//
// The two blocks are separate on purpose - a runtime secret has no business in
// an image layer or a build log - but a build step often needs one value the
// service already has, and re-typing it is how the two drift apart. So a
// reference in the build block may name a build variable, a runtime variable or
// one from an attached group; only the build block's own keys are returned, and
// nothing else is carried into the build.
//
// A name that resolves to nothing is left as written, the same as at runtime,
// so a typo shows up in the build rather than becoming an empty value.
func resolveBuildEnv(buildBlock string, runtime []corev1.EnvVar) (string, []string) {
	build := parseEnvBlock(buildBlock)
	if len(build) == 0 {
		return buildBlock, nil
	}

	// Sources: the build block itself first, so a build variable wins over a
	// runtime one of the same name - the build block is the more specific
	// statement of what this build needs.
	sources := make([]corev1.EnvVar, 0, len(build)+len(runtime))
	sources = append(sources, build...)
	seen := map[string]bool{}
	for _, e := range build {
		seen[e.Name] = true
	}
	for _, e := range runtime {
		if e.ValueFrom != nil || seen[e.Name] {
			continue // a secret reference cannot be read here, and is not needed
		}
		sources = append(sources, e)
	}

	resolved, unknown, looped := resolveEnvRefs(sources)
	byName := make(map[string]string, len(resolved))
	for _, e := range resolved {
		byName[e.Name] = e.Value
	}

	var out strings.Builder
	for _, e := range build {
		out.WriteString(e.Name)
		out.WriteString("=")
		out.WriteString(byName[e.Name])
		out.WriteString("\n")
	}
	return out.String(), append(unknown, looped...)
}

// parseEnvBlock reads KEY=VALUE lines, ignoring blanks and comments.
func parseEnvBlock(block string) []corev1.EnvVar {
	var out []corev1.EnvVar
	for _, line := range strings.Split(block, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, value, ok := strings.Cut(line, "=")
		name = strings.TrimSpace(name)
		if !ok || name == "" {
			continue
		}
		out = append(out, corev1.EnvVar{Name: name, Value: trimQuotes(strings.TrimSpace(value))})
	}
	return out
}

// trimQuotes removes one layer of matching quotes, which is how a value with
// spaces or a "#" is written.
func trimQuotes(v string) string {
	if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
		return v[1 : len(v)-1]
	}
	return v
}

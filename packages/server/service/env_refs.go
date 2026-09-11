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

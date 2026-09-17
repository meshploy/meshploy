package dokploy

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// Decision is a question "needs you" asks, with its choices. Apply refuses to
// start while any decision has no choice. Default is the choice made for you
// until you change it; empty means you must choose.
type Decision struct {
	ID       string   `json:"id"`
	Question string   `json:"question"`
	Options  []Option `json:"options"`
	Default  string   `json:"default,omitempty"`
}

// Option is one answer to a decision.
type Option struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// Choices shared by several kinds of item.
var (
	optImageOnly  = Option{"image_only", "Run the image running now, with no builds until a source is connected"}
	optReconnect  = Option{"reconnect", "Reconnect GitHub in Meshploy first, then build from the repository"}
	optCaddyCert  = Option{"caddy", "Let Caddy issue a certificate"}
	optUploadCert = Option{"upload", "Upload the certificate by hand"}
)

func githubDecision() Decision {
	return Decision{
		ID:       "github",
		Question: "Built from a GitHub App connection, which cannot be moved",
		Options:  []Option{optImageOnly, optReconnect},
		Default:  optImageOnly.ID,
	}
}

func certificateDecision() Decision {
	return Decision{ID: "certificate", Question: "Uses a custom certificate", Options: []Option{optCaddyCert, optUploadCert}, Default: optCaddyCert.ID}
}

// mountDecision asks what to do with a host path an app bind-mounts. Copying
// is the default only when no other container mounts the path: a shared path
// copied into one volume would split what used to be one folder.
func (b *builder) mountDecision(path, appName string) Decision {
	d := Decision{
		ID:      "mount:" + path,
		Options: []Option{{"copy", "Copy its contents into a Meshploy volume"}, {"skip", "Leave the mount out"}},
		Default: "copy",
	}
	size := ""
	if mb, ok := b.src.PathMB[path]; ok {
		size = fmt.Sprintf(", %d MB", mb)
	}
	var sharedWith []string
	for _, c := range b.src.Docker.Containers {
		if c.Service == appName || c.Project == appName || strings.HasPrefix(c.Name, appName+".") {
			continue
		}
		if !c.MountsPath(path) {
			continue
		}
		// A Swarm task or compose container reads better as what owns it.
		name := c.Name
		if c.Service != "" {
			name = c.Service
		} else if c.Project != "" {
			name = c.Project
		}
		if !contains(sharedWith, name) {
			sharedWith = append(sharedWith, name)
		}
	}
	d.Question = "Bind mount of " + path + size
	if len(sharedWith) > 0 {
		d.Question += ", also mounted by " + strings.Join(sharedWith, ", ")
		d.Default = ""
	}
	return d
}

// ── Redirects ────────────────────────────────────────────────────────────────

// Redirect is one hostname sent to another, as a Meshploy route redirect.
type Redirect struct {
	From string `json:"from"`
	To   string `json:"to"`
	Code int    `json:"code"`
}

// dollarRef turns "$1" into "${1}", the form Go expands without ambiguity.
var dollarRef = regexp.MustCompile(`\$(\d+)`)

// resolveRedirects applies Dokploy's regex redirects to an app's own hostnames.
// A rule that sends a hostname to another hostname, keeping the path and
// query, is exactly a Meshploy route redirect. Anything else (a rule that
// rewrites the path, one Go cannot compile, one sending to a host that is not
// migrated) is unresolved.
func resolveRedirects(rules []Row, hosts []string, migrated map[string]bool) (resolved []Redirect, unresolved []string) {
	const probe = "/meshploy-probe/a?b=c"
	for _, rule := range rules {
		pattern := rule.Str("regex")
		re, err := regexp.Compile(pattern)
		if err != nil {
			unresolved = append(unresolved, pattern+" (not a pattern Meshploy can read)")
			continue
		}
		replacement := dollarRef.ReplaceAllString(rule.Str("replacement"), "$${$1}")
		code := 302
		if rule.Bool("permanent") {
			code = 301
		}
		matched := false
		for _, host := range hosts {
			in := "https://" + host + probe
			if !re.MatchString(in) {
				continue
			}
			matched = true
			out, err := url.Parse(re.ReplaceAllString(in, replacement))
			switch {
			case err != nil || out.Host == "" || out.Host == host:
				unresolved = append(unresolved, fmt.Sprintf("%s on %s (does not send it to another host)", pattern, host))
			case out.RequestURI() != probe:
				unresolved = append(unresolved, fmt.Sprintf("%s on %s (changes the path)", pattern, host))
			case !migrated[out.Host]:
				unresolved = append(unresolved, fmt.Sprintf("%s on %s (sends it to %s, which is not a migrated domain)", pattern, host, out.Host))
			default:
				resolved = append(resolved, Redirect{From: host, To: out.Host, Code: code})
			}
		}
		if !matched {
			// A rule naming one of the app's hosts that the probe path did not
			// match applies to certain paths only.
			literal := strings.ReplaceAll(pattern, `\.`, ".")
			reason := "matches none of the app's domains"
			for _, host := range hosts {
				if strings.Contains(literal, host) {
					reason = "applies to some paths of " + host + " only"
					break
				}
			}
			unresolved = append(unresolved, pattern+" ("+reason+")")
		}
	}
	return resolved, unresolved
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

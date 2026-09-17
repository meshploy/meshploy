package hostagent

import (
	"regexp"
	"strings"
)

// ufwAction finds the action column. The "To" column is padded to a width but
// a long value such as "Anywhere on br-b0271be08980" runs into the action with
// a single space, so the columns cannot be split on runs of spaces.
var ufwAction = regexp.MustCompile(`\s(ALLOW|DENY|REJECT|LIMIT)(\s+(IN|OUT|FWD))?\s`)

// ParseUFWStatus reads `ufw status verbose` into a report. Resolve maps an
// application profile to its ports ("22/tcp"), or returns "" when it cannot.
func ParseUFWStatus(out string, resolve func(app string) string) Firewall {
	fw := Firewall{Tool: ToolUFW, DefaultIncoming: "deny"}
	inRules := false
	for _, line := range strings.Split(out, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "Status:"):
			fw.Active = strings.TrimSpace(strings.TrimPrefix(trimmed, "Status:")) == "active"
			continue
		case strings.HasPrefix(trimmed, "Default:"):
			// Default: deny (incoming), allow (outgoing), allow (routed)
			for _, part := range strings.Split(strings.TrimPrefix(trimmed, "Default:"), ",") {
				if policy, dir, ok := strings.Cut(strings.TrimSpace(part), " "); ok && dir == "(incoming)" {
					fw.DefaultIncoming = policy
				}
			}
			continue
		case strings.HasPrefix(trimmed, "--"):
			inRules = true
			continue
		case !inRules || trimmed == "":
			continue
		}
		if r, ok := parseUFWRule(line, resolve); ok {
			fw.Rules = append(fw.Rules, r)
		}
	}
	if !fw.Active {
		fw.Rules = nil
	}
	return fw
}

func parseUFWRule(line string, resolve func(string) string) (Rule, bool) {
	// Drop the comment, then find the action.
	if i := strings.Index(line, " # "); i >= 0 {
		line = line[:i]
	}
	loc := ufwAction.FindStringSubmatchIndex(" " + line + " ")
	if loc == nil {
		return Rule{}, false
	}
	padded := " " + line + " "
	to := strings.TrimSpace(padded[:loc[0]])
	from := strings.TrimSpace(padded[loc[1]:])
	action := strings.ToLower(padded[loc[2]:loc[3]])
	direction := "IN"
	if loc[6] >= 0 {
		direction = padded[loc[6]:loc[7]]
	}
	// Only incoming rules decide who may connect; IPv6 duplicates the IPv4
	// rules ufw writes alongside them.
	if direction != "IN" || strings.Contains(to, "(v6)") || strings.Contains(from, "(v6)") {
		return Rule{}, false
	}

	r := Rule{Action: action, From: "any"}
	if to, iface, ok := strings.Cut(to, " on "); ok {
		r.Interface = strings.TrimSpace(iface)
		return fillTo(r, to, resolve), true
	}
	if f, _, ok := strings.Cut(from, " on "); ok {
		from = f
	}
	if from != "" && from != "Anywhere" {
		// "10.0.0.0/8" or "10.0.0.0/8 22/tcp": the source address comes first.
		r.From = strings.Fields(from)[0]
	}
	return fillTo(r, to, resolve), true
}

// fillTo reads the destination: "22/tcp", "22", "80,443/tcp", "60000:61000/udp",
// "Anywhere", "192.0.2.10 22/tcp" or an application profile.
func fillTo(r Rule, to string, resolve func(string) string) Rule {
	fields := strings.Fields(to)
	if len(fields) == 0 || fields[0] == "Anywhere" {
		return r
	}
	spec := fields[len(fields)-1]
	if !startsWithDigit(spec) {
		// An application profile, such as OpenSSH.
		name := strings.Join(fields, " ")
		if ports := resolve(name); ports != "" {
			spec = ports
		} else {
			r.App = name
			return r
		}
	}
	ports, proto, _ := strings.Cut(spec, "/")
	r.Ports, r.Proto = ports, proto
	return r
}

func startsWithDigit(s string) bool { return s != "" && s[0] >= '0' && s[0] <= '9' }

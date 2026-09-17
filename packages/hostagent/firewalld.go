package hostagent

import "strings"

// ParseFirewalldZone reads `firewall-cmd --zone=<zone> --list-all` for the
// default zone. Services are resolved to their ports with resolve, which
// returns "22/tcp 80/tcp" or "" when it cannot.
func ParseFirewalldZone(out string, resolve func(service string) string) Firewall {
	fw := Firewall{Tool: ToolFirewalld, Active: true, DefaultIncoming: "reject"}
	for _, line := range strings.Split(out, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		switch key {
		case "target":
			// "default" rejects what no rule allows; ACCEPT lets everything in.
			switch value {
			case "ACCEPT":
				fw.DefaultIncoming = "allow"
			case "DROP":
				fw.DefaultIncoming = "deny"
			}
		case "ports":
			for _, p := range strings.Fields(value) {
				fw.Rules = append(fw.Rules, portRule(p))
			}
		case "services":
			for _, svc := range strings.Fields(value) {
				ports := resolve(svc)
				if ports == "" {
					fw.Rules = append(fw.Rules, Rule{Action: "allow", From: "any", App: svc})
					continue
				}
				for _, p := range strings.Fields(ports) {
					fw.Rules = append(fw.Rules, portRule(p))
				}
			}
		case "rich rules":
			if value != "" {
				fw.Unparsed = true
			}
		}
	}
	return fw
}

func portRule(spec string) Rule {
	ports, proto, _ := strings.Cut(spec, "/")
	return Rule{Action: "allow", From: "any", Ports: strings.ReplaceAll(ports, "-", ":"), Proto: proto}
}

package service

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"

	composetypes "github.com/compose-spec/compose-go/v2/types"
	meshdb "github.com/meshploy/packages/db"
	appk8s "github.com/meshploy/packages/server/k8s"
	"gopkg.in/yaml.v3"
)

// What a stack service gets when its x-meshploy block leaves a setting out.
// Apply and FillMeshployDefaults both read these, so the two cannot disagree.
const (
	defaultReplicas    = 1
	defaultDBStorageGB = 10
	// defaultBranch is a build config's branch when none is set; the column
	// defaults to it.
	defaultBranch = "main"
)

// storedSpec is the spec a stack keeps. A pasted one gets its defaults written
// out; a git one is kept as the repo has it, since every sync replaces it.
func storedSpec(spec string, mode meshdb.StackGitMode, repo string) string {
	if mode != meshdb.StackGitModeRaw || repo != "" {
		return spec
	}
	filled, err := FillMeshployDefaults(spec)
	if err != nil {
		return spec // apply reports what is wrong with it
	}
	return filled
}

// FillMeshployDefaults writes out, in each service's x-meshploy block, every
// setting an apply would otherwise default, with the value apply would use. The
// editor's Add Meshploy config, a saved stack and a direct apply then agree, and
// the spec shows what runs. A value already there is never changed.
//
// It inserts lines rather than re-encoding the document, so comments, blank
// lines, quoting and ${VAR} references stay as written. A service it cannot
// read without resolving the file, through a merge key, extends, an anchor or a
// flow-style block, is left for apply to default, which comes to the same
// thing. The spec comes back as it was, with an error, when it is not valid
// YAML or when the result would differ anywhere but in the added settings.
func FillMeshployDefaults(spec string) (string, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(spec), &doc); err != nil {
		return spec, fmt.Errorf("invalid YAML: %w", err)
	}
	if len(doc.Content) == 0 {
		return spec, nil
	}
	services := mappingValue(doc.Content[0], "services")
	if !isBlockMapping(services) {
		return spec, nil
	}

	f := &specFiller{lines: strings.Split(spec, "\n"), after: map[int][]string{}}
	if strings.Contains(spec, "\r\n") {
		f.eol = "\r"
	}
	for i := 0; i+1 < len(services.Content); i += 2 {
		f.service(services.Content[i], services.Content[i+1])
	}
	if len(f.added) == 0 {
		return spec, nil
	}
	filled := f.String()
	if err := checkFilled(spec, filled, f.added); err != nil {
		return spec, err
	}
	return filled, nil
}

// meshploySetting is one x-meshploy setting with the value apply defaults it to.
type meshploySetting struct {
	key   string
	value any // int or string
}

type meshploySection struct {
	name     string
	settings []meshploySetting
}

// meshployDefaults lists, section by section, what apply gives a service whose
// block leaves the setting out. It decides between a database and an app the
// way apply does.
func meshployDefaults(svc, xm *yaml.Node) []meshploySection {
	if database := mappingValue(xm, "database"); scalarValue(xm, "type") == "database" &&
		database != nil && database.Kind == yaml.MappingNode {
		engine := meshdb.DatabaseEngine(scalarValue(database, "engine"))
		if engine == "" {
			engine = meshdb.DatabasePostgres
		}
		settings := []meshploySetting{{"engine", string(engine)}}
		if v := defaultDBVersion(engine); v != "" {
			settings = append(settings, meshploySetting{"version", v})
		}
		settings = append(settings, meshploySetting{"storage_gb", defaultDBStorageGB})
		return []meshploySection{
			{"database", settings},
			{"deploy", []meshploySetting{{"replicas", defaultReplicas}}},
		}
	}

	var deploy []meshploySetting
	if port, ok := composePrimaryPort(svc); ok {
		deploy = append(deploy, meshploySetting{"port", port})
	}
	deploy = append(deploy,
		meshploySetting{"replicas", defaultReplicas},
		meshploySetting{"cpu_request", appk8s.DefaultCPURequest},
		meshploySetting{"cpu_limit", appk8s.DefaultCPULimit},
		meshploySetting{"memory_request", appk8s.DefaultMemoryRequest},
		meshploySetting{"memory_limit", appk8s.DefaultMemoryLimit},
	)
	var sections []meshploySection
	if scalarValue(mappingValue(xm, "source"), "git") != "" {
		sections = append(sections,
			meshploySection{"source", []meshploySetting{{"branch", defaultBranch}}},
			meshploySection{"build", []meshploySetting{
				{"builder", string(meshdb.BuilderNixpacks)},
				{"builder_cpu_request", appk8s.DefaultBuilderCPURequest},
				{"builder_memory_request", appk8s.DefaultBuilderMemoryRequest},
			}},
		)
	}
	return append(sections, meshploySection{"deploy", deploy})
}

// composePrimaryPort is the port apply makes primary from the service's ports:
// and expose:, read with the same stackPorts. ok is false when compose declares
// no port, or when a port can only be read by resolving a variable; apply then
// picks the port itself.
func composePrimaryPort(svc *yaml.Node) (int, bool) {
	var def composetypes.ServiceConfig
	if ports := mappingValue(svc, "ports"); ports != nil {
		if ports.Kind != yaml.SequenceNode {
			return 0, false
		}
		for _, p := range ports.Content {
			switch p.Kind {
			case yaml.ScalarNode:
				if strings.Contains(p.Value, "$") {
					return 0, false
				}
				parsed, err := composetypes.ParsePortConfig(p.Value)
				if err != nil {
					return 0, false
				}
				def.Ports = append(def.Ports, parsed...)
			case yaml.MappingNode:
				pc, ok := longSyntaxPort(p)
				if !ok {
					return 0, false
				}
				def.Ports = append(def.Ports, pc)
			default:
				return 0, false
			}
		}
	}
	if expose := mappingValue(svc, "expose"); expose != nil {
		if expose.Kind != yaml.SequenceNode {
			return 0, false
		}
		for _, e := range expose.Content {
			if e.Kind != yaml.ScalarNode || strings.Contains(e.Value, "$") {
				return 0, false
			}
			def.Expose = append(def.Expose, e.Value)
		}
	}
	ports, declared, _ := stackPorts(def, 0)
	if !declared {
		return 0, false
	}
	for _, p := range ports {
		if p.IsPrimary {
			return p.Port, true
		}
	}
	return 0, false
}

// longSyntaxPort reads a ports: entry in long syntax, the fields stackPorts uses.
func longSyntaxPort(n *yaml.Node) (composetypes.ServicePortConfig, bool) {
	var pc composetypes.ServicePortConfig
	for i := 0; i+1 < len(n.Content); i += 2 {
		v := n.Content[i+1]
		if v.Kind != yaml.ScalarNode || strings.Contains(v.Value, "$") {
			return pc, false
		}
		switch n.Content[i].Value {
		case "target":
			t, err := strconv.ParseUint(v.Value, 10, 32)
			if err != nil {
				return pc, false
			}
			pc.Target = uint32(t)
		case "host_ip":
			pc.HostIP = v.Value
		case "protocol":
			pc.Protocol = v.Value
		case "app_protocol":
			pc.AppProtocol = v.Value
		case "name":
			pc.Name = v.Value
		}
	}
	return pc, pc.Target != 0
}

// specFiller collects the lines to insert, by the index of the line they follow.
type specFiller struct {
	lines []string
	after map[int][]string
	eol   string // "\r" in a file with CRLF line ends
	added []addedSetting
}

type addedSetting struct {
	service, section string
	meshploySetting
}

func (f *specFiller) service(key, svc *yaml.Node) {
	if !isBlockMapping(svc) || svc.Anchor != "" ||
		mappingValue(svc, "<<") != nil || mappingValue(svc, "extends") != nil {
		return
	}
	xm, xmKey := mappingEntry(svc, "x-meshploy")
	sections := meshployDefaults(svc, xm)
	indent := svc.Content[0].Column - 1
	step := indent - (key.Column - 1)
	if step <= 0 {
		step = 2
	}

	if xmKey == nil {
		lines := []string{f.line(indent, "x-meshploy:")}
		for _, sec := range sections {
			lines = append(lines, f.sectionLines(sec.name, sec.settings, indent+step, step)...)
			f.record(key.Value, sec.name, sec.settings)
		}
		f.insert(f.end(key), lines)
		return
	}

	empty := isEmptyNode(xm)
	if !empty && (!isBlockMapping(xm) || xm.Anchor != "") {
		return
	}
	secIndent := indent + step
	if !empty {
		secIndent = xm.Content[0].Column - 1
	}
	secStep := secIndent - (xmKey.Column - 1)
	if secStep <= 0 {
		secStep = 2
	}
	// New sections go after the existing ones, so they are inserted last: a
	// setting added to the last existing section has to come before them.
	var newSections []string
	for _, sec := range sections {
		node, nodeKey := mappingEntry(xm, sec.name)
		switch {
		case nodeKey == nil:
			newSections = append(newSections, f.sectionLines(sec.name, sec.settings, secIndent, secStep)...)
			f.record(key.Value, sec.name, sec.settings)
		case isEmptyNode(node):
			f.insert(f.end(nodeKey), f.settingLines(sec.settings, secIndent+secStep))
			f.record(key.Value, sec.name, sec.settings)
		case isBlockMapping(node) && node.Anchor == "":
			missing := missingSettings(node, sec.settings)
			f.insert(f.end(nodeKey), f.settingLines(missing, node.Content[0].Column-1))
			f.record(key.Value, sec.name, missing)
		}
	}
	f.insert(f.end(xmKey), newSections)
}

// end is the index of the last line of key's entry: the key's line and every
// line after it indented deeper, less trailing blank lines. A line at the key's
// depth or shallower, a comment included, opens the next entry.
func (f *specFiller) end(key *yaml.Node) int {
	indent := key.Column - 1
	last := key.Line - 1
	for i := key.Line; i < len(f.lines); i++ {
		line := strings.TrimRight(f.lines[i], "\r")
		text := strings.TrimLeft(line, " ")
		if text == "" {
			continue
		}
		if len(line)-len(text) <= indent {
			break
		}
		last = i
	}
	return last
}

func (f *specFiller) insert(after int, lines []string) {
	if len(lines) > 0 {
		f.after[after] = append(f.after[after], lines...)
	}
}

func (f *specFiller) record(service, section string, settings []meshploySetting) {
	for _, s := range settings {
		f.added = append(f.added, addedSetting{service, section, s})
	}
}

func (f *specFiller) line(indent int, text string) string {
	return strings.Repeat(" ", indent) + text + f.eol
}

func (f *specFiller) sectionLines(name string, settings []meshploySetting, indent, step int) []string {
	if len(settings) == 0 {
		return nil
	}
	return append([]string{f.line(indent, name+":")}, f.settingLines(settings, indent+step)...)
}

func (f *specFiller) settingLines(settings []meshploySetting, indent int) []string {
	var lines []string
	for _, s := range settings {
		lines = append(lines, f.line(indent, s.key+": "+yamlValue(s.value)))
	}
	return lines
}

func (f *specFiller) String() string {
	out := make([]string, 0, len(f.lines)+len(f.added)+8)
	for i, l := range f.lines {
		out = append(out, l)
		out = append(out, f.after[i]...)
	}
	return strings.Join(out, "\n")
}

// yamlValue writes a setting's value. A string that would read as something
// else unquoted, as "8.0" reads as the number 8, is quoted.
func yamlValue(v any) string {
	switch v := v.(type) {
	case int:
		return strconv.Itoa(v)
	case string:
		var parsed any
		if err := yaml.Unmarshal([]byte(v), &parsed); err == nil {
			if s, ok := parsed.(string); ok && s == v {
				return v
			}
		}
		return strconv.Quote(v)
	}
	return fmt.Sprint(v)
}

func missingSettings(section *yaml.Node, settings []meshploySetting) []meshploySetting {
	var missing []meshploySetting
	for _, s := range settings {
		if _, k := mappingEntry(section, s.key); k == nil {
			missing = append(missing, s)
		}
	}
	return missing
}

// checkFilled makes sure the fill changed nothing but what it added: each added
// setting reads back where it belongs, and without them the documents match.
func checkFilled(before, after string, added []addedSetting) error {
	var b, a map[string]any
	if err := yaml.Unmarshal([]byte(before), &b); err != nil {
		return err
	}
	if err := yaml.Unmarshal([]byte(after), &a); err != nil {
		return fmt.Errorf("writing out the defaults would break the YAML: %w", err)
	}
	for _, s := range added {
		sec, _ := childMap(childMap(childMap(a, "services"), s.service), "x-meshploy")[s.section].(map[string]any)
		if got, ok := sec[s.key]; !ok || fmt.Sprint(got) != fmt.Sprint(s.value) {
			return fmt.Errorf("writing out the defaults misplaced %s's %s.%s", s.service, s.section, s.key)
		}
		delete(sec, s.key)
	}
	// Blocks the fill created, or filled from empty, are empty maps now. Put
	// them back as they were: absent, or null.
	bServices := childMap(b, "services")
	for name, v := range childMap(a, "services") {
		svc, _ := v.(map[string]any)
		xm, ok := svc["x-meshploy"].(map[string]any)
		if !ok {
			continue
		}
		bxm, hadXM := childMap(bServices, name)["x-meshploy"]
		for secName, sv := range xm {
			if m, ok := sv.(map[string]any); ok && len(m) == 0 {
				old, had := childMap(childMap(bServices, name), "x-meshploy")[secName]
				switch {
				case !had:
					delete(xm, secName)
				case old == nil:
					xm[secName] = nil
				}
			}
		}
		if len(xm) == 0 {
			switch {
			case !hadXM:
				delete(svc, "x-meshploy")
			case bxm == nil:
				svc["x-meshploy"] = nil
			}
		}
	}
	if !reflect.DeepEqual(b, a) {
		return fmt.Errorf("writing out the defaults would change more than x-meshploy")
	}
	return nil
}

func childMap(m map[string]any, key string) map[string]any {
	c, _ := m[key].(map[string]any)
	return c
}

func mappingEntry(n *yaml.Node, key string) (value, keyNode *yaml.Node) {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil, nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1], n.Content[i]
		}
	}
	return nil, nil
}

func mappingValue(n *yaml.Node, key string) *yaml.Node {
	v, _ := mappingEntry(n, key)
	return v
}

func scalarValue(n *yaml.Node, key string) string {
	if v := mappingValue(n, key); v != nil && v.Kind == yaml.ScalarNode {
		return v.Value
	}
	return ""
}

func isBlockMapping(n *yaml.Node) bool {
	return n != nil && n.Kind == yaml.MappingNode && n.Style&yaml.FlowStyle == 0 && len(n.Content) > 0
}

// isEmptyNode is a key written with nothing after it, which a fill can give
// children.
func isEmptyNode(n *yaml.Node) bool {
	return n != nil && n.Kind == yaml.ScalarNode && n.Tag == "!!null" && n.Value == ""
}

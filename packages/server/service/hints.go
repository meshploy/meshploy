package service

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
	"k8s.io/apimachinery/pkg/api/resource"
)

// Hints are advice about a service from what its last build found out about
// the app, set against how the service is configured: a start command the
// builder could not work out, dependencies that need more memory than the
// limit allows, a port the app does not listen on. Each comes with the fix,
// and goes away once the configuration no longer matches it, or when someone
// dismisses it.
//
// The builder reports the app as "Stack:" lines (apps/builder/meshploy-build),
// which recordStack keeps on the deployment; Railpack and Nixpacks say for
// themselves when they found no start command.

// Hint kinds.
const (
	HintStartCommand = "start_command" // the builder found nothing to run
	HintMemory       = "memory"        // dependencies known to need more than the limit
	HintPort         = "port"          // the app listens somewhere the service does not send traffic
)

// Hint fix actions: what the console changes when the fix is taken.
const (
	HintFixStartCommand = "start_command" // set the start command to Value
	HintFixMemory       = "memory"        // set the memory limit to Value
	HintFixPort         = "port"          // set the primary port to Value
	HintFixBuilder      = "builder"       // build with Value (dockerfile)
)

// Hint is one piece of advice about a service.
type Hint struct {
	Kind   string    `json:"kind"`
	Title  string    `json:"title"`
	Detail string    `json:"detail"`
	Fixes  []HintFix `json:"fixes,omitempty"`
	// DeploymentID is the build the advice comes from, when it comes from one.
	DeploymentID *uuid.UUID `json:"deployment_id,omitempty"`
}

// HintFix is one way to act on a hint.
type HintFix struct {
	Action string `json:"action"`
	Label  string `json:"label"`
	Value  string `json:"value"`
}

// StackFacts is what a build found out about the app.
type StackFacts struct {
	Languages      []string `json:"languages,omitempty"`
	Framework      string   `json:"framework,omitempty"` // fastapi, flask, django, node
	Entry          string   `json:"entry,omitempty"`     // app.main:app, mysite.wsgi, server.js
	Heavy          []string `json:"heavy,omitempty"`
	Dockerfile     string   `json:"dockerfile,omitempty"`
	DockerfileCmd  bool     `json:"dockerfile_cmd,omitempty"`
	Expose         int      `json:"expose,omitempty"`
	NoStartCommand bool     `json:"no_start_command,omitempty"`
}

func (f StackFacts) empty() bool {
	return len(f.Languages) == 0 && f.Framework == "" && len(f.Heavy) == 0 &&
		f.Dockerfile == "" && f.Expose == 0 && !f.NoStartCommand
}

var (
	stackLine      = regexp.MustCompile(`Stack: ([a-z]+) ?([^\r\n\x1b]*)`)
	noStartCommand = regexp.MustCompile(`(?i)no start command`)
)

// stackFromBuildLog reads the facts out of a build's log.
func stackFromBuildLog(log string) StackFacts {
	var f StackFacts
	for _, m := range stackLine.FindAllStringSubmatch(log, -1) {
		args := strings.Fields(m[2])
		switch m[1] {
		case "language":
			if len(args) > 0 && !slices.Contains(f.Languages, args[0]) {
				f.Languages = append(f.Languages, args[0])
			}
		case "entry":
			if len(args) >= 2 {
				f.Framework, f.Entry = args[0], args[1]
			}
		case "heavy":
			f.Heavy = args
		case "dockerfile":
			if len(args) > 0 {
				f.Dockerfile = args[0]
				f.DockerfileCmd = len(args) > 1 && args[1] == "cmd"
			}
		case "expose":
			if len(args) > 0 {
				f.Expose, _ = strconv.Atoi(args[0])
			}
		}
	}
	f.NoStartCommand = noStartCommand.MatchString(log)
	return f
}

// recordStack keeps what a build found out on its deployment.
func (s *DeploymentService) recordStack(ctx context.Context, deploymentID uuid.UUID, log string) {
	f := stackFromBuildLog(log)
	if f.empty() {
		return
	}
	b, err := json.Marshal(f)
	if err != nil {
		return
	}
	s.db.WithContext(ctx).Model(&db.Deployment{}).Where("id = ?", deploymentID).Update("stack_facts", string(b))
}

// heavyMemory is what each dependency the builder looks for usually needs, at
// the least. Kept in step with the builder's list.
var heavyMemory = map[string]string{
	"torch":                 "2Gi",
	"tensorflow":            "2Gi",
	"jax":                   "2Gi",
	"transformers":          "2Gi",
	"sentence-transformers": "2Gi",
	"vllm":                  "2Gi",
	"llama-cpp-python":      "2Gi",
	"spacy":                 "1Gi",
	"easyocr":               "2Gi",
	"paddleocr":             "2Gi",
	"onnxruntime":           "1Gi",
	"puppeteer":             "1Gi",
	"playwright":            "1Gi",
}

// startSuggestion is a start command for the entry point the builder found.
func startSuggestion(f StackFacts) string {
	switch f.Framework {
	case "fastapi":
		return "uvicorn " + f.Entry + " --host 0.0.0.0 --port $PORT"
	case "flask":
		return "gunicorn " + f.Entry + " --bind 0.0.0.0:$PORT"
	case "django":
		return "gunicorn " + f.Entry + " --bind 0.0.0.0:$PORT"
	case "node":
		return "node " + f.Entry
	}
	return ""
}

var frameworkName = map[string]string{"fastapi": "FastAPI", "flask": "Flask", "django": "Django", "node": "Node.js"}

// entryFile is where the entry point lives, for the words: app.main:app is
// app/main.py.
func entryFile(f StackFacts) string {
	switch f.Framework {
	case "fastapi", "flask", "django":
		mod, _, _ := strings.Cut(f.Entry, ":")
		return strings.ReplaceAll(mod, ".", "/") + ".py"
	}
	return f.Entry
}

// hardPort finds a port written into a start command rather than read from
// $PORT: --port 8080, -p 8080, --bind 0.0.0.0:8080, -b :8080.
var hardPort = regexp.MustCompile(`(?:--port[= ]|-p |--bind[= ]|-b )(?:[0-9.]*:)?([0-9]{2,5})\b`)

// hintsFor is the advice for one service, from its last build's facts.
func hintsFor(svc db.Service, bc *db.BuildConfig, f StackFacts, from *uuid.UUID) []Hint {
	var out []Hint
	builder := db.BuilderType("")
	if bc != nil {
		builder = bc.Builder
	}
	builtBy := "The builder"
	if builder == db.BuilderRailpack {
		builtBy = "Railpack"
	}

	if f.NoStartCommand && svc.StartCommand == "" && builder != db.BuilderDockerfile {
		h := Hint{Kind: HintStartCommand, Title: "No start command", DeploymentID: from,
			Detail: builtBy + " could not tell how to start this app, so its container has nothing to run."}
		if cmd := startSuggestion(f); cmd != "" {
			h.Detail += fmt.Sprintf(" It looks like a %s app in %s.", frameworkName[f.Framework], entryFile(f))
			h.Fixes = append(h.Fixes, HintFix{Action: HintFixStartCommand, Label: "Set start command", Value: cmd})
		} else {
			h.Fixes = append(h.Fixes, HintFix{Action: HintFixStartCommand, Label: "Set start command"})
		}
		if f.Dockerfile != "" && f.DockerfileCmd {
			h.Detail += " The repository has a Dockerfile that says how to start it."
			h.Fixes = append(h.Fixes, HintFix{Action: HintFixBuilder, Label: "Build with its Dockerfile", Value: string(db.BuilderDockerfile)})
		}
		out = append(out, h)
	}

	if need, pkgs := memoryNeed(f.Heavy); need != "" {
		limit, err := resource.ParseQuantity(svc.MemoryLimit)
		want := resource.MustParse(need)
		if svc.MemoryLimit == "" || (err == nil && limit.Cmp(want) < 0) {
			current := svc.MemoryLimit
			if current == "" {
				current = "unset"
			}
			out = append(out, Hint{Kind: HintMemory, Title: "Likely to need more memory", DeploymentID: from,
				Detail: fmt.Sprintf("It depends on %s, which usually needs %s or more; its memory limit is %s.",
					joinNames(pkgs), humanQuantity(need), humanQuantity(current)),
				Fixes: []HintFix{{Action: HintFixMemory, Label: "Set limit to " + humanQuantity(need), Value: need}}})
		}
	}

	if primary := primaryPortOf(svc.Ports); primary != 0 {
		switch {
		case svc.StartCommand != "":
			if m := hardPort.FindStringSubmatch(svc.StartCommand); m != nil {
				if p, _ := strconv.Atoi(m[1]); p != 0 && p != primary {
					out = append(out, Hint{Kind: HintPort, Title: "Listens on a different port",
						Detail: fmt.Sprintf("The start command listens on %d, but the service sends traffic to %d.", p, primary),
						Fixes: []HintFix{
							{Action: HintFixPort, Label: fmt.Sprintf("Use port %d", p), Value: strconv.Itoa(p)},
							{Action: HintFixStartCommand, Label: "Listen on $PORT", Value: strings.Replace(svc.StartCommand, m[0], strings.Replace(m[0], m[1], "$PORT", 1), 1)},
						}})
				}
			}
		case builder == db.BuilderDockerfile && f.Expose != 0 && f.Expose != primary:
			out = append(out, Hint{Kind: HintPort, Title: "Listens on a different port", DeploymentID: from,
				Detail: fmt.Sprintf("Its Dockerfile says the app listens on %d (EXPOSE), but the service sends traffic to %d.", f.Expose, primary),
				Fixes:  []HintFix{{Action: HintFixPort, Label: fmt.Sprintf("Use port %d", f.Expose), Value: strconv.Itoa(f.Expose)}}})
		}
	}

	dismissed := strings.Split(svc.DismissedHints, ",")
	return slices.DeleteFunc(out, func(h Hint) bool { return slices.Contains(dismissed, h.Kind) })
}

// memoryNeed is the most any of these dependencies needs, and the ones that
// need it.
func memoryNeed(heavy []string) (string, []string) {
	var need resource.Quantity
	var needS string
	var pkgs []string
	for _, h := range heavy {
		q, ok := heavyMemory[h]
		if !ok {
			continue
		}
		v := resource.MustParse(q)
		switch c := v.Cmp(need); {
		case needS == "" || c > 0:
			need, needS, pkgs = v, q, []string{h}
		case c == 0:
			pkgs = append(pkgs, h)
		}
	}
	return needS, pkgs
}

// humanQuantity writes 2Gi as 2 GiB and 512Mi as 512 MiB.
func humanQuantity(q string) string {
	for _, u := range []string{"Gi", "Mi", "Ki"} {
		if n, ok := strings.CutSuffix(q, u); ok {
			return n + " " + u + "B"
		}
	}
	return q
}

func primaryPortOf(ports []db.ServicePort) int {
	for _, p := range ports {
		if p.IsPrimary {
			return p.Port
		}
	}
	if len(ports) > 0 {
		return ports[0].Port
	}
	return 0
}

// Hints is the advice for each of a project's services that has some.
func (s *WorkloadService) Hints(ctx context.Context, projectID uuid.UUID) (map[uuid.UUID][]Hint, error) {
	var services []db.Service
	if err := s.db.WithContext(ctx).Preload("Ports").
		Where("project_id = ? AND type = ?", projectID, db.ServiceTypeApplication).
		Find(&services).Error; err != nil {
		return nil, err
	}
	out := map[uuid.UUID][]Hint{}
	for _, svc := range services {
		var bc *db.BuildConfig
		var row db.BuildConfig
		if s.db.WithContext(ctx).Where("service_id = ?", svc.ID).First(&row).Error == nil {
			bc = &row
		}
		var f StackFacts
		var from *uuid.UUID
		var dep db.Deployment
		if bc != nil && s.db.WithContext(ctx).Select("id", "stack_facts").
			Where("service_id = ? AND source = ? AND stack_facts <> ''", svc.ID, db.DeploySourceBuild).
			Order("created_at DESC").First(&dep).Error == nil {
			_ = json.Unmarshal([]byte(dep.StackFacts), &f)
			from = &dep.ID
		}
		if hs := hintsFor(svc, bc, f, from); len(hs) > 0 {
			out[svc.ID] = hs
		}
	}
	return out, nil
}

// DismissHint sets one kind of advice aside for a service, for everyone.
func (s *WorkloadService) DismissHint(ctx context.Context, serviceID uuid.UUID, kind string) error {
	switch kind {
	case HintStartCommand, HintMemory, HintPort:
	default:
		return fmt.Errorf("unknown hint %q", kind)
	}
	var svc db.Service
	if err := s.db.WithContext(ctx).Select("id", "dismissed_hints").First(&svc, "id = ?", serviceID).Error; err != nil {
		return err
	}
	kinds := slices.DeleteFunc(strings.Split(svc.DismissedHints, ","), func(k string) bool { return k == "" })
	if slices.Contains(kinds, kind) {
		return nil
	}
	return s.db.WithContext(ctx).Model(&db.Service{}).Where("id = ?", serviceID).
		Update("dismissed_hints", strings.Join(append(kinds, kind), ",")).Error
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

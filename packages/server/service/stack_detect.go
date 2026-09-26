package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
)

// Reading a repository before its first build, to fill in the new service
// form with what fits the app: the builder, the port, a start command, the
// memory it will need.
//
// It looks for the same things the builder reports after a build (see
// apps/builder/meshploy-build and hints.go), from a shallow clone that
// fetches the tree and small files only, so it works the same with every git
// provider and costs little on a large repository. Nothing here is binding:
// the form shows what was found and every value stays editable, and the
// after-build hints catch what a look at a few files cannot see.

// ErrGitIntegrationNotFound is a git integration that is not the org's.
var ErrGitIntegrationNotFound = errors.New("git integration not found")

// Detection is what a look at a repository found, and what it suggests.
type Detection struct {
	Facts StackFacts `json:"facts"`
	// Summary says what the app is, in a line: Python · FastAPI (app/main.py) · Dockerfile.
	Summary string `json:"summary"`
	// The suggested settings; empty when there is nothing better than the
	// form's own default.
	Builder      string `json:"builder,omitempty"`
	Port         int    `json:"port,omitempty"`
	StartCommand string `json:"start_command,omitempty"`
	MemoryLimit  string `json:"memory_limit,omitempty"`
	// Notes explain a suggestion, or say what could not be worked out.
	Notes []string `json:"notes,omitempty"`
}

// repoView is a repository's files, by path, with their sizes, and a way to
// read the small ones.
type repoView struct {
	sizes map[string]int64
	read  func(p string) string
}

func (r repoView) has(p string) bool { _, ok := r.sizes[p]; return ok }

// maxDetectRead is the largest file detection reads; the clone fetches
// nothing bigger.
const maxDetectRead = 200 << 10

// skipDirs are never an app's own code.
var skipDirs = map[string]bool{".git": true, "node_modules": true, ".venv": true, "venv": true, "__pycache__": true,
	"dist": true, "build": true, "tests": true, "test": true, "vendor": true, ".next": true}

func skipped(p string) bool {
	for _, part := range strings.Split(path.Dir(p), "/") {
		if skipDirs[part] {
			return true
		}
	}
	return false
}

var (
	heavyPattern   = regexp.MustCompile(`(?:^|[^a-z0-9-])(torch|tensorflow|jax|transformers|sentence-transformers|spacy|easyocr|paddleocr|onnxruntime|llama-cpp-python|vllm|puppeteer|playwright)(?:[^a-z0-9-]|$)`)
	exposePattern  = regexp.MustCompile(`(?im)^\s*EXPOSE\s+([0-9]+)`)
	cmdPattern     = regexp.MustCompile(`(?im)^\s*(CMD|ENTRYPOINT)\s`)
	appObject      = map[string]*regexp.Regexp{}
	nodeStartEntry = []string{"server.js", "index.js", "main.js", "app.js", "dist/index.js", "dist/main.js"}
)

func init() {
	for _, fw := range []string{"FastAPI", "Flask"} {
		appObject[fw] = regexp.MustCompile(`(?m)^([A-Za-z_][A-Za-z0-9_]*)\s*(?::[^=\n]*)?=\s*` + fw + `\(`)
	}
}

// detectStack reads the facts out of a repository's files, as the builder does.
func detectStack(r repoView, dockerfilePath string) StackFacts {
	var f StackFacts
	var deps strings.Builder

	python := false
	for _, p := range []string{"requirements.txt", "pyproject.toml", "Pipfile", "setup.py"} {
		if r.has(p) {
			python = true
		}
	}
	if python {
		f.Languages = append(f.Languages, "python")
		for p := range r.sizes {
			if !strings.Contains(p, "/") && (strings.HasPrefix(p, "requirements") && strings.HasSuffix(p, ".txt") ||
				p == "pyproject.toml" || p == "Pipfile" || p == "setup.py") {
				deps.WriteString(strings.NewReplacer("_", "-").Replace(strings.ToLower(r.read(p))) + "\n")
			}
		}
		// The ASGI or WSGI app object, shallowest file first.
		var py []string
		for p := range r.sizes {
			if strings.HasSuffix(p, ".py") && strings.Count(p, "/") < 4 && !skipped(p) {
				py = append(py, p)
			}
		}
		sort.Slice(py, func(i, j int) bool {
			if di, dj := strings.Count(py[i], "/"), strings.Count(py[j], "/"); di != dj {
				return di < dj
			}
			return py[i] < py[j]
		})
	search:
		for _, fw := range []string{"FastAPI", "Flask"} {
			for _, p := range py {
				if m := appObject[fw].FindStringSubmatch(r.read(p)); m != nil {
					f.Framework = strings.ToLower(fw)
					f.Entry = strings.ReplaceAll(strings.TrimSuffix(p, ".py"), "/", ".") + ":" + m[1]
					break search
				}
			}
		}
		if f.Framework == "" && r.has("manage.py") {
			for _, p := range py {
				if path.Base(p) == "wsgi.py" {
					f.Framework, f.Entry = "django", strings.ReplaceAll(strings.TrimSuffix(p, ".py"), "/", ".")
					break
				}
			}
		}
	}

	if r.has("package.json") {
		f.Languages = append(f.Languages, "node")
		pkg := r.read("package.json")
		deps.WriteString(pkg + "\n")
		var manifest struct {
			Scripts map[string]string `json:"scripts"`
		}
		_ = json.Unmarshal([]byte(pkg), &manifest)
		if manifest.Scripts["start"] == "" {
			for _, p := range nodeStartEntry {
				if r.has(p) {
					f.Framework, f.Entry = "node", p
					break
				}
			}
		}
	}
	for _, l := range [][2]string{{"go.mod", "go"}, {"Gemfile", "ruby"}, {"composer.json", "php"}, {"Cargo.toml", "rust"}} {
		if r.has(l[0]) {
			f.Languages = append(f.Languages, l[1])
		}
	}

	seen := map[string]bool{}
	for _, m := range heavyPattern.FindAllStringSubmatch(deps.String(), -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			f.Heavy = append(f.Heavy, m[1])
		}
	}
	sort.Strings(f.Heavy)

	if dockerfilePath == "" {
		dockerfilePath = "Dockerfile"
	}
	if r.has(dockerfilePath) {
		f.Dockerfile = dockerfilePath
		text := r.read(dockerfilePath)
		f.DockerfileCmd = cmdPattern.MatchString(text)
		if m := exposePattern.FindStringSubmatch(text); m != nil {
			f.Expose, _ = strconv.Atoi(m[1])
		}
	}
	return f
}

// frameworkPort is the port each framework's usual start command listens on
// when nothing says otherwise. The suggested start commands read $PORT, which
// the service sets to its own port, so this only has to be a sensible default.
var frameworkPort = map[string]int{"fastapi": 8000, "django": 8000, "flask": 5000, "node": 3000}

var languageName = map[string]string{"python": "Python", "node": "Node.js", "go": "Go", "ruby": "Ruby", "php": "PHP", "rust": "Rust"}

// suggest turns the facts into settings for a new service, by the same rules
// the after-build hints use.
func suggest(f StackFacts) Detection {
	d := Detection{Facts: f}
	var parts []string
	for _, l := range f.Languages {
		parts = append(parts, languageName[l])
	}
	if f.Framework != "" && f.Framework != "node" {
		parts = append(parts, fmt.Sprintf("%s (%s)", frameworkName[f.Framework], entryFile(f)))
	}
	if f.Dockerfile != "" {
		parts = append(parts, f.Dockerfile)
	}
	d.Summary = strings.Join(parts, " · ")

	if f.Dockerfile != "" && f.DockerfileCmd {
		d.Builder = string(db.BuilderDockerfile)
		d.Notes = append(d.Notes, "Builds with the repository's "+f.Dockerfile+", which says how to start the app.")
		if f.Expose != 0 {
			d.Port = f.Expose
		}
	} else {
		d.Builder = string(db.BuilderRailpack)
		d.StartCommand = startSuggestion(f)
		if d.StartCommand != "" {
			d.Port = frameworkPort[f.Framework]
			d.Notes = append(d.Notes, fmt.Sprintf("Starts the %s app in %s; check the command before deploying.", frameworkName[f.Framework], entryFile(f)))
		} else if len(f.Languages) > 0 && f.Languages[0] == "python" {
			d.Notes = append(d.Notes, "No app entry point found; if the build cannot tell how to start it, set a start command.")
		}
	}
	if need, pkgs := memoryNeed(f.Heavy); need != "" {
		d.MemoryLimit = need
		d.Notes = append(d.Notes, fmt.Sprintf("Depends on %s, which usually needs %s or more.", joinNames(pkgs), humanQuantity(need)))
	}
	return d
}

// DetectStack looks at a repository's branch and suggests how to build and
// run it. integrationID is nil for a public repository, and must be orgID's.
// A branch or dockerfile path starting with "-" is refused, since both reach
// git's command line.
func (s *GitIntegrationService) DetectStack(ctx context.Context, orgID uuid.UUID, integrationID *uuid.UUID, repo, branch, dockerfilePath string) (*Detection, error) {
	var integration *db.GitIntegration
	if integrationID != nil {
		var row db.GitIntegration
		if err := s.db.WithContext(ctx).First(&row, "id = ? AND organization_id = ?", *integrationID, orgID).Error; err != nil {
			return nil, ErrGitIntegrationNotFound
		}
		integration = &row
	}
	if strings.HasPrefix(branch, "-") || strings.HasPrefix(repo, "-") {
		return nil, fmt.Errorf("invalid repository or branch")
	}
	creds, err := s.cloneCredentials(ctx, integration, repo)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	r, cleanup, err := shallowRepoView(ctx, creds, branch)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	d := suggest(detectStack(r, dockerfilePath))
	return &d, nil
}

// shallowRepoView clones the branch's tree and its small files, without
// checking anything out, and reads them from git's store.
func shallowRepoView(ctx context.Context, creds gitCredentials, branch string) (repoView, func(), error) {
	dir, err := os.MkdirTemp("", "meshploy-detect-*")
	if err != nil {
		return repoView{}, nil, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	clone := exec.CommandContext(ctx, "git", "clone", "--quiet", "--depth", "1", "--no-checkout",
		"--filter=blob:limit="+strconv.Itoa(maxDetectRead+1), "--branch", branch, creds.URL, dir)
	clone.Env = append(os.Environ(), creds.gitEnv()...)
	if out, err := clone.CombinedOutput(); err != nil {
		cleanup()
		return repoView{}, nil, fmt.Errorf("could not read the repository: %s", strings.TrimSpace(lastLine(string(out))))
	}
	ls := exec.CommandContext(ctx, "git", "-C", dir, "ls-tree", "-r", "-l", "HEAD")
	out, err := ls.Output()
	if err != nil {
		cleanup()
		return repoView{}, nil, fmt.Errorf("could not list the repository: %w", err)
	}
	sizes := map[string]int64{}
	blobs := map[string]string{}
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		// <mode> blob <sha> <size>\t<path>
		meta, p, ok := strings.Cut(sc.Text(), "\t")
		fields := strings.Fields(meta)
		if !ok || len(fields) != 4 || fields[1] != "blob" {
			continue
		}
		size, _ := strconv.ParseInt(fields[3], 10, 64)
		sizes[p] = size
		blobs[p] = fields[2]
	}
	read := func(p string) string {
		sha, ok := blobs[p]
		if !ok || sizes[p] > maxDetectRead {
			return ""
		}
		// Only blobs the clone fetched are read: GIT_NO_LAZY_FETCH keeps a
		// missing one from being fetched, on a git that knows it.
		cmd := exec.CommandContext(ctx, "git", "-C", dir, "cat-file", "blob", sha)
		cmd.Env = append(os.Environ(), "GIT_NO_LAZY_FETCH=1", "GIT_TERMINAL_PROMPT=0")
		b, err := cmd.Output()
		if err != nil {
			return ""
		}
		return string(b)
	}
	return repoView{sizes: sizes, read: read}, cleanup, nil
}

func lastLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		return s[i+1:]
	}
	return s
}

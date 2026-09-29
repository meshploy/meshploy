package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	composetypes "github.com/compose-spec/compose-go/v2/types"
	meshdb "github.com/meshploy/packages/db"
)

// stackFileSource reads a file a compose definition names by file:, given the
// path as written, relative to the compose file. nil means no files are
// available to this apply.
type stackFileSource func(rel string) ([]byte, error)

// maxStackFile is the largest file a stack can put into a container: config
// files are mounted from a Kubernetes Secret, which holds at most 1 MiB.
const maxStackFile = 1 << 20

// maxBindFiles caps the files one bind-mounted directory can carry.
const maxBindFiles = 200

// maxRepoRead caps any single read from a checkout.
const maxRepoRead = 4 << 20

// mapFileSource serves the files a client sent with its manifest, which is how
// `meshploy apply` carries files the server cannot see.
func mapFileSource(files map[string]string) stackFileSource {
	if len(files) == 0 {
		return nil
	}
	byPath := make(map[string]string, len(files))
	for p, c := range files {
		byPath[path.Clean(filepath.ToSlash(p))] = c
	}
	return func(rel string) ([]byte, error) {
		want := path.Clean(filepath.ToSlash(rel))
		c, ok := byPath[want]
		if !ok {
			var under []string
			for p := range byPath {
				if rest, found := strings.CutPrefix(p, want+"/"); found {
					under = append(under, rest)
				}
			}
			if len(under) > 0 {
				sort.Strings(under)
				return nil, &dirError{files: under}
			}
			return nil, fmt.Errorf("not sent with the manifest")
		}
		return []byte(c), nil
	}
}

// dirError is what a file source answers when the path it was asked for is a
// directory: the files under it, relative to it. A bind mount of a directory
// becomes one config file per file.
type dirError struct{ files []string }

func (e *dirError) Error() string { return "is a directory" }

// gitFileSource reads the files a git stack's compose file names from the same
// repository and branch, fetched the way its spec is. cleanup removes the
// checkout a whole-repo stack makes for them.
func (s *StackService) gitFileSource(ctx context.Context, stack *meshdb.Stack) (stackFileSource, func()) {
	var (
		creds    gitCredentials
		credErr  error
		haveCred bool
		dir      string
		cloneErr error
		cloned   bool
	)
	credentials := func() (gitCredentials, error) {
		if !haveCred {
			haveCred = true
			creds, credErr = s.git.cloneCredentials(ctx, stack.GitIntegration, stack.GitRepo)
		}
		return creds, credErr
	}
	src := func(rel string) ([]byte, error) {
		p, err := repoPath(stack.GitPath, rel)
		if err != nil {
			return nil, err
		}
		c, err := credentials()
		if err != nil {
			return nil, err
		}
		if stack.GitMode == meshdb.StackGitModeFile {
			content, _, err := fetchRawFile(ctx, stack.GitRepo, stack.GitBranch, p, c.Token)
			if err != nil {
				return nil, err
			}
			return []byte(content), nil
		}
		if !cloned {
			cloned = true
			dir, cloneErr = cloneRepo(ctx, c, stack.GitBranch)
		}
		if cloneErr != nil {
			return nil, cloneErr
		}
		return readRepoFile(dir, p)
	}
	cleanup := func() {
		if dir != "" {
			_ = os.RemoveAll(dir)
		}
	}
	return src, cleanup
}

// repoPath resolves a path written in a compose file against the compose
// file's place in the repository, so infra/docker-compose.yml naming
// ../shared/realm.json reads shared/realm.json. A path that leaves the
// repository is refused.
func repoPath(composePath, rel string) (string, error) {
	rel = filepath.ToSlash(rel)
	if path.IsAbs(rel) {
		return "", fmt.Errorf("%s is an absolute path; name it relative to the compose file", rel)
	}
	p := path.Join(path.Dir(strings.TrimPrefix(path.Clean(filepath.ToSlash(composePath)), "/")), rel)
	if !fs.ValidPath(p) {
		return "", fmt.Errorf("%s is outside the repository", rel)
	}
	return p, nil
}

// readRepoFile reads p from a checkout through os.Root, so neither the path nor
// a symlink in the repository can reach a file outside it, such as the API's
// own configuration.
func readRepoFile(dir, p string) ([]byte, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	f, err := root.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if info, err := f.Stat(); err == nil && info.IsDir() {
		var under []string
		walkErr := fs.WalkDir(root.FS(), p, func(q string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.Type().IsRegular() {
				under = append(under, strings.TrimPrefix(q, p+"/"))
			}
			if len(under) > maxBindFiles {
				return fmt.Errorf("%s holds more than %d files", p, maxBindFiles)
			}
			return nil
		})
		if walkErr != nil {
			return nil, walkErr
		}
		sort.Strings(under)
		return nil, &dirError{files: under}
	}
	data, err := io.ReadAll(io.LimitReader(f, maxRepoRead+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxRepoRead {
		return nil, fmt.Errorf("%s is larger than %d MiB", p, maxRepoRead>>20)
	}
	return data, nil
}

// composeFiles turns the compose configs: and secrets: a service uses into the
// files Meshploy projects into its container, where compose would put them: a
// config at its target, /<name> by default, and a secret at /run/secrets/<name>.
// Both are stored encrypted, as config files.
//
// The content comes from content:, from environment:, or from file: through
// files. A file this apply cannot read keeps the copy an earlier apply stored,
// so applying a stack from the console does not undo one applied with its
// files. notes say what was kept or left out.
func (s *StackService) composeFiles(
	ctx context.Context,
	stack meshdb.Stack,
	project *composetypes.Project,
	svcDef composetypes.ServiceConfig,
	env map[string]string,
	rawPaths map[string]string,
	files stackFileSource,
) ([]meshployFile, []string) {
	var out []meshployFile
	var notes []string
	add := func(kind, name, target string, obj composetypes.FileObjectConfig, defined bool) {
		label := fmt.Sprintf("%s %q", strings.TrimSuffix(kind, "s"), name)
		switch {
		case !defined:
			notes = append(notes, label+" left out: it is not defined under "+kind+":")
		case bool(obj.External):
			notes = append(notes, label+" left out: external "+kind+" are not supported")
		case obj.File != "":
			rel := rawPaths[kind+"/"+name]
			if rel == "" {
				rel = obj.File
			}
			reason := noFilesReason(stack)
			if files != nil {
				data, err := files(rel)
				switch {
				case err == nil && len(data) > maxStackFile:
					notes = append(notes, fmt.Sprintf("%s left out: %s is larger than 1 MiB, the most a config file can hold", label, rel))
					return
				case err == nil:
					out = append(out, meshployFile{Path: target, Content: string(data)})
					return
				default:
					reason = err.Error()
				}
			}
			if s.hasStackFile(ctx, stack, target) {
				notes = append(notes, fmt.Sprintf("%s kept the copy an earlier apply stored: %s could not be read (%s)", label, rel, reason))
				return
			}
			notes = append(notes, fmt.Sprintf("%s left out: %s could not be read (%s); apply with `meshploy apply -f`, from git, or give it content:", label, rel, reason))
		case obj.Environment != "":
			v := obj.Content
			if v == "" {
				v = env[obj.Environment]
			}
			if v == "" {
				notes = append(notes, fmt.Sprintf("%s left out: stack variable %s is not set", label, obj.Environment))
				return
			}
			out = append(out, meshployFile{Path: target, Content: v})
		default:
			out = append(out, meshployFile{Path: target, Content: obj.Content})
		}
	}

	// Bind mounts of the repository's own files - ./migrations, a script, a
	// settings file - become config files at the same place: the files are in
	// the repository the stack came from, where compose read them. Anything
	// else a bind mount names is on the machine that ran compose, and is left
	// out with the reason.
	var bindBytes int
	for _, v := range svcDef.Volumes {
		if v.Type != composetypes.VolumeTypeBind {
			continue
		}
		rel := rawPaths["binds/"+svcDef.Name+"/"+v.Target]
		if rel == "" {
			rel = v.Source
		}
		if !strings.HasPrefix(rel, "./") && !strings.HasPrefix(rel, "../") && rel != "." {
			notes = append(notes, fmt.Sprintf("bind mount of %s at %s left out: it is a path on the machine that runs compose; use configs: for a file, or a named volume", rel, v.Target))
			continue
		}
		label := fmt.Sprintf("bind mount of %s at %s", rel, v.Target)
		if files == nil {
			notes = append(notes, label+" "+noFilesNote(stack))
			continue
		}
		take := func(from, to string) bool {
			data, err := files(from)
			if err != nil {
				notes = append(notes, fmt.Sprintf("%s left out: %s could not be read (%v)", label, from, err))
				return false
			}
			bindBytes += len(data)
			if bindBytes > maxStackFile {
				notes = append(notes, fmt.Sprintf("%s left out: its files come to more than 1 MiB, the most a service's config files can hold; use a named volume", label))
				return false
			}
			out = append(out, meshployFile{Path: to, Content: string(data)})
			return true
		}
		data, err := files(rel)
		var dir *dirError
		switch {
		case err == nil:
			bindBytes += len(data)
			if bindBytes > maxStackFile {
				notes = append(notes, fmt.Sprintf("%s left out: it is larger than 1 MiB, the most a config file can hold", label))
				continue
			}
			out = append(out, meshployFile{Path: v.Target, Content: string(data)})
		case errors.As(err, &dir):
			for _, f := range dir.files {
				if !take(path.Join(rel, f), path.Join(v.Target, f)) {
					break
				}
			}
		default:
			notes = append(notes, fmt.Sprintf("%s left out: it could not be read (%v)", label, err))
		}
	}

	for _, ref := range svcDef.Configs {
		obj, defined := project.Configs[ref.Source]
		target := ref.Target
		if target == "" {
			target = ref.Source
		}
		if !path.IsAbs(target) {
			target = "/" + target
		}
		add("configs", ref.Source, target, composetypes.FileObjectConfig(obj), defined)
	}
	for _, ref := range svcDef.Secrets {
		obj, defined := project.Secrets[ref.Source]
		target := ref.Target
		if target == "" {
			target = ref.Source
		}
		if !path.IsAbs(target) {
			target = "/run/secrets/" + target
		}
		add("secrets", ref.Source, target, composetypes.FileObjectConfig(obj), defined)
	}
	return out, notes
}

func (s *StackService) hasStackFile(ctx context.Context, stack meshdb.Stack, target string) bool {
	var n int64
	s.db.WithContext(ctx).Model(&meshdb.ConfigFile{}).
		Where("project_id = ? AND stack_id = ? AND path = ?", stack.ProjectID, stack.ID, target).
		Count(&n)
	return n > 0
}

// droppedByStack names what a compose service asks for that a stack cannot
// carry over, so an apply says so rather than leaving it out silently.
func droppedByStack(svcDef composetypes.ServiceConfig) []string {
	var notes []string
	if b := svcDef.Build; b != nil {
		if b.Target != "" {
			notes = append(notes, fmt.Sprintf("build target %q left out: the whole Dockerfile is built", b.Target))
		}
		if b.DockerfileInline != "" {
			notes = append(notes, "dockerfile_inline left out: put the Dockerfile in the repository")
		}
		for k, v := range b.Args {
			if v == nil {
				notes = append(notes, fmt.Sprintf("build arg %s has no value here and is left out", k))
			}
		}
	}
	if len(svcDef.ExtraHosts) > 0 {
		notes = append(notes, "extra_hosts left out: services reach each other by name")
	}
	keys := make([]string, 0, len(svcDef.Environment))
	for k := range svcDef.Environment {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if v := svcDef.Environment[k]; v != nil && strings.Contains(*v, "host.docker.internal") {
			notes = append(notes, fmt.Sprintf("%s points at host.docker.internal, the machine running Docker, which does not exist here; point it at a service by name", k))
		}
	}
	return notes
}

// rawBindSources reads each service's bind mounts from the compose YAML as
// written, keyed "binds/<service>/<target>": the short form ("./x:/x:ro")
// and the long one (source/target). Nothing is interpolated or resolved.
func rawBindSources(spec string) map[string]string {
	var doc struct {
		Services map[string]struct {
			Volumes []any `yaml:"volumes"`
		} `yaml:"services"`
	}
	out := map[string]string{}
	if yaml.Unmarshal([]byte(spec), &doc) != nil {
		return out
	}
	for name, svc := range doc.Services {
		for _, v := range svc.Volumes {
			var src, target string
			switch e := v.(type) {
			case string:
				parts := strings.Split(e, ":")
				if len(parts) < 2 {
					continue
				}
				src, target = parts[0], parts[1]
			case map[string]any:
				if t, _ := e["type"].(string); t != "" && t != "bind" {
					continue
				}
				src, _ = e["source"].(string)
				target, _ = e["target"].(string)
			}
			if src == "" || target == "" {
				continue
			}
			// A named volume has no path in it; only a path is a bind.
			if !strings.HasPrefix(src, ".") && !strings.HasPrefix(src, "/") && !strings.HasPrefix(src, "~") {
				continue
			}
			out["binds/"+name+"/"+path.Clean(target)] = src
		}
	}
	return out
}

// fromGit says a stack's files come from its repository, fetched by a sync.
func fromGit(stack meshdb.Stack) bool {
	return stack.GitMode != meshdb.StackGitModeRaw && stack.GitRepo != ""
}

// noFilesNote is what an apply with no files says about one: a git stack's
// apply does not fetch - that is what a sync is for - and keeps what the last
// sync fetched, which "left out" made sound removed.
func noFilesNote(stack meshdb.Stack) string {
	if fromGit(stack) {
		return "kept as the last sync fetched it: an apply does not fetch, sync to take newer files"
	}
	return "left out: " + noFilesReason(stack)
}

func noFilesReason(stack meshdb.Stack) string {
	if fromGit(stack) {
		return "an apply does not fetch from the repository, so it keeps what the last sync fetched; sync to take newer files"
	}
	return "no files were sent or fetched with this apply"
}

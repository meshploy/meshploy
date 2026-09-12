package service

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

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
		c, ok := byPath[path.Clean(filepath.ToSlash(rel))]
		if !ok {
			return nil, fmt.Errorf("not sent with the manifest")
		}
		return []byte(c), nil
	}
}

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
			reason := "no files were sent or fetched with this apply"
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
	for _, v := range svcDef.Volumes {
		if v.Type == composetypes.VolumeTypeBind {
			notes = append(notes, fmt.Sprintf("bind mount at %s left out: the files it names are on the machine that runs compose; use configs: for a file, or a named volume", v.Target))
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

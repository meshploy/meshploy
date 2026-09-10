package k8s

import (
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/meshploy/packages/server/internal/gittest"
	"github.com/meshploy/packages/server/version"
)

// builderScript is the script the builder image runs, from this repository.
const builderScript = "../../../apps/builder/meshploy-build"

func TestDefaultBuilderImageFollowsTheChannel(t *testing.T) {
	orig := version.Channel
	t.Cleanup(func() { version.Channel = orig })
	for channel, want := range map[string]string{
		"edge":   "ghcr.io/meshploy/builder:main",
		"stable": "ghcr.io/meshploy/builder:latest",
		"dev":    "ghcr.io/meshploy/builder:latest",
	} {
		version.Channel = channel
		if got := DefaultBuilderImage(); got != want {
			t.Errorf("channel %s: got %s, want %s", channel, got, want)
		}
	}
}

// jobEnv submits a build job to a fake cluster and returns its image and the
// environment its builder container gets.
func jobEnv(t *testing.T, p BuildJobParams) (image string, env map[string]string) {
	t.Helper()
	client := fake.NewSimpleClientset()
	if err := CreateBuildJob(t.Context(), client, p); err != nil {
		t.Fatal(err)
	}
	job, err := client.BatchV1().Jobs(p.Namespace).Get(t.Context(), p.JobName, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	c := job.Spec.Template.Spec.Containers[0]
	env = map[string]string{}
	for _, e := range c.Env {
		env[e.Name] = e.Value
	}
	return c.Image, env
}

func TestCreateBuildJobPassesTheImageAndGitSource(t *testing.T) {
	image, env := jobEnv(t, BuildJobParams{
		JobName: "build-app-1", Namespace: "demo", Image: "example.com/builder:test",
		GitRepo: "group/sub/app", GitURL: "https://git.example.com/group/sub/app.git",
		GitUser: "oauth2", GitToken: "tok", GitBranch: "main",
	})
	if image != "example.com/builder:test" {
		t.Errorf("image = %s; BUILDER_IMAGE would be ignored again", image)
	}
	for k, want := range map[string]string{
		"GIT_URL": "https://git.example.com/group/sub/app.git", "GIT_USER": "oauth2",
		"GIT_TOKEN": "tok", "GIT_REPO": "group/sub/app", "GIT_BRANCH": "main",
	} {
		if env[k] != want {
			t.Errorf("%s = %q, want %q", k, env[k], want)
		}
	}

	if image, _ := jobEnv(t, BuildJobParams{JobName: "build-app-2", Namespace: "demo"}); image != DefaultBuilderImage() {
		t.Errorf("image with none given = %s, want %s", image, DefaultBuilderImage())
	}
}

// runBuilder runs the builder script's clone with the given environment. The
// builder "none" is not a real one, so the script stops right after cloning.
func runBuilder(t *testing.T, home string, env map[string]string) (out, workdir string) {
	t.Helper()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash is not installed")
	}
	workdir = filepath.Join(t.TempDir(), "workspace")
	cmd := exec.Command("bash", builderScript)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "MESHPLOY_BUILD_WORKDIR=" + workdir}
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	b, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(b), "Unknown builder") {
		t.Fatalf("expected the script to clone and then stop at the unknown builder, got %v:\n%s", err, b)
	}
	if !strings.Contains(string(b), "Cloned successfully") {
		t.Fatalf("the clone failed:\n%s", b)
	}
	return string(b), workdir
}

func basicAuth(user, token string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+token))
}

// The contract between the job and the script it runs: the environment
// CreateBuildJob builds is what the builder script needs to clone, with the
// token sent as a header and left nowhere in the clone or the log.
func TestBuilderScriptClonesWithTheJobsEnvironment(t *testing.T) {
	url, seen := gittest.Serve(t)
	const token = "s3cret-token"
	_, env := jobEnv(t, BuildJobParams{
		JobName: "build-app-1", Namespace: "demo", GitRepo: "group/app",
		GitURL: url, GitUser: "oauth2", GitToken: token, GitBranch: "main",
		Builder: "none", ImageDest: "registry.example/app:1", RegistryHost: "registry.example",
	})

	out, workdir := runBuilder(t, t.TempDir(), env)

	for i, h := range seen() {
		if h != basicAuth("oauth2", token) {
			t.Errorf("request %d sent Authorization %q", i, h)
		}
	}
	cfg, _ := os.ReadFile(filepath.Join(workdir, ".git", "config"))
	if strings.Contains(string(cfg), token) || strings.Contains(out, token) {
		t.Errorf("the token leaked into .git/config or the build log:\n%s\n%s", cfg, out)
	}
}

func TestBuilderScriptClonesAPublicRepositoryWithoutAToken(t *testing.T) {
	url, seen := gittest.Serve(t)
	runBuilder(t, t.TempDir(), map[string]string{
		"GIT_URL": url, "GIT_REPO": url, "GIT_BRANCH": "main", "GIT_TOKEN": "",
		"BUILDER": "none", "IMAGE_DEST": "registry.example/app:1", "REGISTRY_HOST": "registry.example",
	})
	for _, h := range seen() {
		if h != "" {
			t.Errorf("anonymous clone sent Authorization %q", h)
		}
	}
}

// An API from before GIT_URL existed sends only GIT_REPO as owner/repo, meaning
// github.com. A git config that redirects github.com to the local server stands
// in for GitHub.
func TestBuilderScriptStillAcceptsWhatAnOlderAPISends(t *testing.T) {
	url, seen := gittest.Serve(t)
	home := t.TempDir()
	base := strings.TrimSuffix(url, "repo.git")
	gitconfig := "[url \"" + base + "\"]\n\tinsteadOf = https://github.com/\n"
	if err := os.WriteFile(filepath.Join(home, ".gitconfig"), []byte(gitconfig), 0o644); err != nil {
		t.Fatal(err)
	}

	runBuilder(t, home, map[string]string{
		"GIT_REPO": "repo", "GIT_BRANCH": "main", "GIT_TOKEN": "installation-token",
		"BUILDER": "none", "IMAGE_DEST": "registry.example/app:1", "REGISTRY_HOST": "registry.example",
	})
	for i, h := range seen() {
		if h != basicAuth("x-access-token", "installation-token") {
			t.Errorf("request %d sent Authorization %q", i, h)
		}
	}
}

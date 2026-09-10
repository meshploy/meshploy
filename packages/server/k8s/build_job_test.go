package k8s

import (
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
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

// ── WaitForJobWith ───────────────────────────────────────────────────────────

// buildJobWithPod submits a build job to a fake cluster and gives it a pod in
// the state set up by pod.
func buildJobWithPod(t *testing.T, pod func(*corev1.Pod)) *fake.Clientset {
	t.Helper()
	client := fake.NewSimpleClientset()
	if err := CreateBuildJob(t.Context(), client, BuildJobParams{JobName: "build-app-1", Namespace: "demo"}); err != nil {
		t.Fatal(err)
	}
	p := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Name: "build-app-1-abcde", Namespace: "demo", Labels: map[string]string{"job-name": "build-app-1"},
	}}
	pod(p)
	if _, err := client.CoreV1().Pods("demo").Create(t.Context(), p, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	return client
}

func unschedulable(message string) func(*corev1.Pod) {
	return func(p *corev1.Pod) {
		p.Status.Phase = corev1.PodPending
		p.Status.Conditions = []corev1.PodCondition{{
			Type: corev1.PodScheduled, Status: corev1.ConditionFalse,
			Reason: corev1.PodReasonUnschedulable, Message: message,
		}}
	}
}

// The case this exists for: with no node marked for builds, the pod stayed
// Pending and the deploy showed "Building" for the whole hour-long timeout.
func TestWaitForJobGivesUpWhenNoNodeMatches(t *testing.T) {
	client := buildJobWithPod(t, unschedulable("0/1 nodes are available: 1 node(s) didn't match Pod's node affinity/selector."))

	res := WaitForJobWith(t.Context(), client, "demo", "build-app-1", JobWaitOptions{
		Timeout: 5 * time.Second, Unschedulable: 50 * time.Millisecond, Poll: 10 * time.Millisecond,
	})
	if !res.Unschedulable || res.Success || !strings.Contains(res.Log, "didn't match") {
		t.Fatalf("got %+v, want an unschedulable result carrying the scheduler's reason", res)
	}
}

// A pod short of CPU or memory is queued behind other builds, not stranded.
func TestWaitForJobKeepsWaitingForCapacity(t *testing.T) {
	client := buildJobWithPod(t, unschedulable("0/1 nodes are available: 1 Insufficient cpu."))

	res := WaitForJobWith(t.Context(), client, "demo", "build-app-1", JobWaitOptions{
		Timeout: 200 * time.Millisecond, Unschedulable: 20 * time.Millisecond, Poll: 10 * time.Millisecond,
	})
	if res.Unschedulable || !strings.Contains(res.Log, "timed out") {
		t.Fatalf("got %+v, want it to keep waiting until the timeout", res)
	}
}

func TestWaitForJobReportsWhereTheBuildStarted(t *testing.T) {
	client := buildJobWithPod(t, func(p *corev1.Pod) {
		p.Spec.NodeName = "srv1854405"
		p.Status.Phase = corev1.PodRunning
	})
	job, err := client.BatchV1().Jobs("demo").Get(t.Context(), "build-app-1", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	job.Status.Succeeded = 1
	if _, err := client.BatchV1().Jobs("demo").UpdateStatus(t.Context(), job, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}

	var started []string
	res := WaitForJobWith(t.Context(), client, "demo", "build-app-1", JobWaitOptions{
		Timeout: 5 * time.Second, Unschedulable: time.Minute, Poll: 10 * time.Millisecond,
		OnStarted: func(node string) { started = append(started, node) },
	})
	if !res.Success {
		t.Fatalf("got %+v, want success", res)
	}
	if len(started) != 1 || started[0] != "srv1854405" {
		t.Errorf("OnStarted calls = %v, want one for srv1854405", started)
	}
}

func TestIsBuildNode(t *testing.T) {
	if !IsBuildNode(map[string]string{"meshploy.com/role": "builder"}) {
		t.Error("the builder label was not recognised")
	}
	if IsBuildNode(map[string]string{"kubernetes.io/hostname": "srv"}) || IsBuildNode(nil) {
		t.Error("a node without the label counted as a build node")
	}
}

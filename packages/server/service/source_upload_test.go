package service_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	meshdb "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/config"
	"github.com/meshploy/packages/server/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// sourceRegistry holds blobs and manifests the way registry:2 does, as much as
// an upload needs: blob uploads in one request, checked against their digest,
// and manifests that must name blobs it has.
type sourceRegistry struct {
	mu        sync.Mutex
	blobs     map[string][]byte // digest -> content
	manifests map[string][]byte // repo:tag -> manifest
	deleted   []string          // repo@digest
	blobPuts  int
}

func newSourceRegistry(t *testing.T) (*sourceRegistry, string) {
	r := &sourceRegistry{blobs: map[string][]byte{}, manifests: map[string][]byte{}}
	srv := httptest.NewServer(http.HandlerFunc(r.serve))
	t.Cleanup(srv.Close)
	return r, strings.TrimPrefix(srv.URL, "http://")
}

func (r *sourceRegistry) serve(w http.ResponseWriter, req *http.Request) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p := strings.TrimPrefix(req.URL.Path, "/v2/")
	switch {
	case strings.Contains(p, "/blobs/uploads/") && req.Method == http.MethodPost:
		w.Header().Set("Location", "/v2/"+p+uuid.NewString())
		w.WriteHeader(http.StatusAccepted)
	case strings.Contains(p, "/blobs/uploads/") && req.Method == http.MethodPut:
		body, _ := io.ReadAll(req.Body)
		digest := req.URL.Query().Get("digest")
		if fmt.Sprintf("sha256:%x", sha256.Sum256(body)) != digest {
			http.Error(w, "digest mismatch", http.StatusBadRequest)
			return
		}
		r.blobs[digest] = body
		r.blobPuts++
		w.WriteHeader(http.StatusCreated)
	case strings.Contains(p, "/blobs/"):
		digest := p[strings.LastIndex(p, "/")+1:]
		if _, ok := r.blobs[digest]; !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	case strings.Contains(p, "/manifests/"):
		i := strings.Index(p, "/manifests/")
		repo, ref := p[:i], p[i+len("/manifests/"):]
		switch req.Method {
		case http.MethodPut:
			body, _ := io.ReadAll(req.Body)
			var m struct {
				Config struct{ Digest string }
				Layers []struct{ Digest string }
			}
			_ = json.Unmarshal(body, &m)
			for _, d := range append([]string{m.Config.Digest}, func() (ds []string) {
				for _, l := range m.Layers {
					ds = append(ds, l.Digest)
				}
				return
			}()...) {
				if _, ok := r.blobs[d]; !ok {
					http.Error(w, "blob unknown "+d, http.StatusBadRequest)
					return
				}
			}
			r.manifests[repo+":"+ref] = body
			w.WriteHeader(http.StatusCreated)
		case http.MethodHead:
			m, ok := r.manifests[repo+":"+ref]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("Docker-Content-Digest", fmt.Sprintf("sha256:%x", sha256.Sum256(m)))
			w.WriteHeader(http.StatusOK)
		case http.MethodDelete:
			for k, m := range r.manifests {
				if strings.HasPrefix(k, repo+":") && fmt.Sprintf("sha256:%x", sha256.Sum256(m)) == ref {
					delete(r.manifests, k)
					r.deleted = append(r.deleted, k)
				}
			}
			w.WriteHeader(http.StatusAccepted)
		}
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (r *sourceRegistry) tags() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for k := range r.manifests {
		out = append(out, k)
	}
	return out
}

// packFolder is a folder as the CLI and console send it.
func packFolder(t *testing.T, entries ...*tar.Header) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	for _, h := range entries {
		body := []byte("content of " + h.Name)
		if h.Typeflag == tar.TypeReg || h.Typeflag == 0 {
			h.Size = int64(len(body))
		}
		if h.Mode == 0 {
			h.Mode = 0o644
		}
		require.NoError(t, tw.WriteHeader(h))
		if h.Size > 0 {
			_, err := tw.Write(body)
			require.NoError(t, err)
		}
	}
	require.NoError(t, tw.Close())
	require.NoError(t, zw.Close())
	return buf.Bytes()
}

func file(name string) *tar.Header { return &tar.Header{Name: name, Typeflag: tar.TypeReg} }

type uploadFixture struct {
	svcs    *service.Services
	gdb     *gorm.DB
	reg     *sourceRegistry
	project meshdb.Project
	svc     meshdb.Service
}

func newUploadFixture(t *testing.T) uploadFixture {
	t.Helper()
	gdb := newTestDB(t)
	svcs := service.New(gdb, &config.Config{})
	reg, host := newSourceRegistry(t)
	org := meshdb.Organization{Name: "up", Slug: "up-" + uuid.NewString()[:8]}
	require.NoError(t, gdb.Create(&org).Error)
	require.NoError(t, gdb.Create(&meshdb.RegistryIntegration{OrganizationID: org.ID, Name: "Built-in",
		Provider: meshdb.RegistryBuiltin, Endpoint: host}).Error)
	project := meshdb.Project{OrganizationID: org.ID, Name: "Tools", Slug: "tools-" + uuid.NewString()[:6]}
	require.NoError(t, gdb.Create(&project).Error)
	svc, err := svcs.Workloads.Create(context.Background(), project.ID, service.CreateWorkloadInput{Name: "my-app", FromUpload: true})
	require.NoError(t, err)
	return uploadFixture{svcs: svcs, gdb: gdb, reg: reg, project: project, svc: *svc}
}

func (f uploadFixture) buildConfig(t *testing.T) meshdb.BuildConfig {
	t.Helper()
	var bc meshdb.BuildConfig
	require.NoError(t, f.gdb.First(&bc, "service_id = ?", f.svc.ID).Error)
	return bc
}

// A service made to be built from a folder has a build config without a
// repository, keeping its last images for rollback like one made from Git.
func TestAServiceFromAFolderHasABuildConfigWithoutARepository(t *testing.T) {
	f := newUploadFixture(t)
	bc := f.buildConfig(t)
	assert.Empty(t, bc.GitRepo)
	assert.Equal(t, meshdb.BuilderRailpack, bc.Builder)
	assert.True(t, bc.RollbackEnabled)
	assert.False(t, bc.BuildsFromUpload(), "nothing uploaded yet")
}

// An upload is stored in the service's registry, recorded on its build config,
// and the upload before it is untagged; the same folder twice is stored once.
func TestAnUploadIsStoredAndReplacesTheOneBefore(t *testing.T) {
	f := newUploadFixture(t)
	ctx := context.Background()
	first := packFolder(t, file("package.json"), file("src/index.js"))

	src, err := f.svcs.Deployments.UploadSource(ctx, f.svc.ID, "/home/me/code/my-app", bytes.NewReader(first))
	require.NoError(t, err)
	assert.Equal(t, fmt.Sprintf("sha256:%x", sha256.Sum256(first)), src.Digest)
	assert.Equal(t, "my-app", src.Name)
	assert.Equal(t, 2, src.Files)
	assert.Equal(t, int64(len(first)), src.Size)

	bc := f.buildConfig(t)
	assert.True(t, bc.BuildsFromUpload())
	assert.Equal(t, src.Digest, bc.UploadDigest)
	repo := f.project.Slug + "-" + bc.UploadRepo[len(f.project.Slug)+1:]
	assert.Equal(t, []string{repo + ":src-" + src.Digest[7:19]}, f.reg.tags())

	puts := f.reg.blobPuts
	_, err = f.svcs.Deployments.UploadSource(ctx, f.svc.ID, "my-app", bytes.NewReader(first))
	require.NoError(t, err)
	assert.Equal(t, puts, f.reg.blobPuts, "the registry had it already")

	second := packFolder(t, file("package.json"), file("src/index.js"), file("src/more.js"))
	src2, err := f.svcs.Deployments.UploadSource(ctx, f.svc.ID, "my-app", bytes.NewReader(second))
	require.NoError(t, err)
	assert.Equal(t, []string{repo + ":src-" + src2.Digest[7:19]}, f.reg.tags(), "the first upload is untagged")
}

// A copy in another level building from the same upload keeps it tagged when
// the original moves on.
func TestAnUploadAnotherServiceBuildsFromIsKept(t *testing.T) {
	f := newUploadFixture(t)
	ctx := context.Background()
	first := packFolder(t, file("main.go"))
	src, err := f.svcs.Deployments.UploadSource(ctx, f.svc.ID, "app", bytes.NewReader(first))
	require.NoError(t, err)
	bc := f.buildConfig(t)
	other, err := f.svcs.Workloads.Create(ctx, f.project.ID, service.CreateWorkloadInput{Name: "copy", FromUpload: true})
	require.NoError(t, err)
	require.NoError(t, f.gdb.Model(&meshdb.BuildConfig{}).Where("service_id = ?", other.ID).
		Updates(map[string]any{"upload_repo": bc.UploadRepo, "upload_digest": src.Digest}).Error)

	_, err = f.svcs.Deployments.UploadSource(ctx, f.svc.ID, "app", bytes.NewReader(packFolder(t, file("main.go"), file("go.mod"))))
	require.NoError(t, err)
	assert.Contains(t, f.reg.tags(), bc.UploadRepo+":src-"+src.Digest[7:19])
}

// What is not a folder Meshploy can build is refused before anything is
// stored.
func TestAnUploadThatIsNotABuildableFolderIsRefused(t *testing.T) {
	f := newUploadFixture(t)
	ctx := context.Background()
	for name, archive := range map[string][]byte{
		"not gzip":         []byte("just some text"),
		"empty":            packFolder(t, &tar.Header{Name: "src/", Typeflag: tar.TypeDir, Mode: 0o755}),
		"parent path":      packFolder(t, file("ok.txt"), file("../../etc/cron.d/x")),
		"absolute path":    packFolder(t, file("/etc/passwd")),
		"absolute symlink": packFolder(t, file("ok.txt"), &tar.Header{Name: "link", Typeflag: tar.TypeSymlink, Linkname: "/etc"}),
		"escaping symlink": packFolder(t, file("ok.txt"), &tar.Header{Name: "a/link", Typeflag: tar.TypeSymlink, Linkname: "../../.."}),
		"device":           packFolder(t, file("ok.txt"), &tar.Header{Name: "dev", Typeflag: tar.TypeChar}),
	} {
		_, err := f.svcs.Deployments.UploadSource(ctx, f.svc.ID, "app", bytes.NewReader(archive))
		assert.ErrorIs(t, err, service.ErrUploadInvalid, name)
	}
	assert.Empty(t, f.reg.tags())
	assert.False(t, f.buildConfig(t).BuildsFromUpload())

	// A link inside the folder is an ordinary part of an app.
	_, err := f.svcs.Deployments.UploadSource(ctx, f.svc.ID, "app", bytes.NewReader(
		packFolder(t, file("bin/real"), &tar.Header{Name: "bin/alias", Typeflag: tar.TypeSymlink, Linkname: "real"})))
	assert.NoError(t, err)
}

// A database is not built, so it takes no folder.
func TestADatabaseTakesNoUpload(t *testing.T) {
	f := newUploadFixture(t)
	require.NoError(t, f.gdb.Model(&meshdb.Service{}).Where("id = ?", f.svc.ID).Update("type", meshdb.ServiceTypeDatabase).Error)
	_, err := f.svcs.Deployments.UploadSource(context.Background(), f.svc.ID, "app", bytes.NewReader(packFolder(t, file("a"))))
	assert.ErrorIs(t, err, service.ErrUploadNotApplication)
}

// A deploy of a service with an upload and no repository builds the upload:
// the build job is told where to fetch it, and nothing about Git; a
// repository set later wins.
func TestADeployBuildsTheUploadedFolder(t *testing.T) {
	f := newUploadFixture(t)
	ctx := context.Background()
	client := fake.NewSimpleClientset(&corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "gw", Labels: map[string]string{"meshploy.com/role": "builder"}},
		Status:     corev1.NodeStatus{Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}}},
	})
	service.UseK8sForTest(f.svcs, client)
	src, err := f.svcs.Deployments.UploadSource(ctx, f.svc.ID, "my-app", bytes.NewReader(packFolder(t, file("index.js"))))
	require.NoError(t, err)

	dep, err := f.svcs.Deployments.Trigger(ctx, service.TriggerInput{ServiceID: f.svc.ID, TriggeredBy: uuid.New()})
	require.NoError(t, err)
	assert.Equal(t, meshdb.DeploySourceBuild, dep.Source)
	assert.Empty(t, dep.SourceBranch, "an uploaded folder has no branch")

	env := waitForBuildEnv(t, client, f.project.Slug, dep.BuildJobName)
	bc := f.buildConfig(t)
	assert.Equal(t, src.Digest, env["SOURCE_DIGEST"])
	assert.Equal(t, "my-app", env["SOURCE_NAME"])
	assert.True(t, strings.HasPrefix(env["SOURCE_URL"], "http://"))
	assert.True(t, strings.HasSuffix(env["SOURCE_URL"], "/v2/"+bc.UploadRepo+"/blobs/"+src.Digest))
	assert.Empty(t, env["GIT_URL"])

	require.NoError(t, f.gdb.Model(&meshdb.BuildConfig{}).Where("id = ?", bc.ID).Update("git_repo", "https://github.com/acme/app").Error)
	dep2, err := f.svcs.Deployments.Trigger(ctx, service.TriggerInput{ServiceID: f.svc.ID, TriggeredBy: uuid.New()})
	require.NoError(t, err)
	env = waitForBuildEnv(t, client, f.project.Slug, dep2.BuildJobName)
	assert.Empty(t, env["SOURCE_URL"], "a repository wins")
	assert.Equal(t, "https://github.com/acme/app", env["GIT_URL"])
}

func waitForBuildEnv(t *testing.T, client *fake.Clientset, ns, name string) map[string]string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		job, err := client.BatchV1().Jobs(ns).Get(context.Background(), name, metav1.GetOptions{})
		if err == nil {
			env := map[string]string{}
			for _, e := range job.Spec.Template.Spec.Containers[0].Env {
				env[e.Name] = e.Value
			}
			return env
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("no build job %s/%s", ns, name)
	return nil
}

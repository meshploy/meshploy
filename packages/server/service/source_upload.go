package service

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"github.com/google/uuid"
	db "github.com/meshploy/packages/db"
	"gorm.io/gorm"
)

// Folder uploads: a service built from a folder sent as a tar.gz rather than
// cloned from a repository. The archive is kept in the registry the service's
// images go to, as the one layer of an OCI artifact in a repository of its
// own: every build node already reaches that registry, the blob is addressed
// by its digest, and the registry's garbage collection removes what is no
// longer tagged. The build fetches the blob by digest, checks it, and unpacks
// it where it would have cloned.

const (
	// MaxUploadBytes is the largest packed folder taken. The CLI and console
	// leave out .git, node_modules and .env files, so an app over this is
	// almost always carrying something it should not.
	MaxUploadBytes = 100 << 20
	// maxUnpackedBytes bounds what the archive unpacks to, so a small archive
	// cannot fill the build node's disk.
	maxUnpackedBytes = 1 << 30

	ociManifestType = "application/vnd.oci.image.manifest.v1+json"
	ociLayerType    = "application/vnd.oci.image.layer.v1.tar+gzip"
	// uploadConfigType marks the artifact as a Meshploy source, not an image
	// anything could run.
	uploadConfigType = "application/vnd.meshploy.source.config.v1+json"
)

var (
	// ErrUploadTooLarge is an archive over MaxUploadBytes.
	ErrUploadTooLarge = fmt.Errorf("the folder is over %d MB packed: leave out build output and dependencies", MaxUploadBytes>>20)
	// ErrUploadInvalid is an archive that is not a folder Meshploy can build.
	ErrUploadInvalid = errors.New("not a folder Meshploy can build")
	// ErrUploadNotApplication is an upload to a database or other service
	// that is not built.
	ErrUploadNotApplication = errors.New("only an application service is built from a folder")
)

// UploadedSource is what an upload stored.
type UploadedSource struct {
	Digest     string    `json:"digest"`
	Name       string    `json:"name"`
	Size       int64     `json:"size"`
	Files      int       `json:"files"`
	UploadedAt time.Time `json:"uploaded_at"`
}

// UploadSource stores a folder, packed as a tar.gz, as the service's source.
// The service gets a build config if it has none, building with Railpack the
// way a new service from Git does; one with a repository keeps it, and the
// upload is kept but not built until the repository is cleared.
func (s *DeploymentService) UploadSource(ctx context.Context, serviceID uuid.UUID, name string, body io.Reader) (*UploadedSource, error) {
	var svc db.Service
	if err := s.db.WithContext(ctx).Preload("Project").First(&svc, "id = ?", serviceID).Error; err != nil {
		return nil, fmt.Errorf("service not found: %w", err)
	}
	if svc.Type != db.ServiceTypeApplication {
		return nil, ErrUploadNotApplication
	}

	f, err := os.CreateTemp("", "meshploy-upload-*.tar.gz")
	if err != nil {
		return nil, err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	sum := sha256.New()
	size, err := io.Copy(io.MultiWriter(f, sum), io.LimitReader(body, MaxUploadBytes+1))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return nil, ErrUploadTooLarge
		}
		return nil, fmt.Errorf("read upload: %w", err)
	}
	if size > MaxUploadBytes {
		return nil, ErrUploadTooLarge
	}
	digest := "sha256:" + hex.EncodeToString(sum.Sum(nil))
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	files, err := checkUploadArchive(f)
	if err != nil {
		return nil, err
	}

	bc, err := s.buildConfigForUpload(ctx, &svc)
	if err != nil {
		return nil, err
	}
	host, user, pass, err := s.resolveRegistry(ctx, bc)
	if err != nil {
		return nil, fmt.Errorf("registry: %w", err)
	}
	name = uploadName(name)
	repo := fmt.Sprintf("%s-%s-source", svc.Project.Slug, appK8sName(&svc))
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	reg := registryClient{host: host, user: user, pass: pass}
	if err := reg.pushSource(repo, uploadTag(digest), name, f, digest, size); err != nil {
		return nil, fmt.Errorf("store the folder in the registry %s: %w", host, err)
	}

	prevRepo, prevDigest := bc.UploadRepo, bc.UploadDigest
	now := time.Now().UTC()
	if err := s.db.WithContext(ctx).Model(&db.BuildConfig{}).Where("id = ?", bc.ID).Updates(map[string]any{
		"upload_repo":   repo,
		"upload_digest": digest,
		"upload_name":   name,
		"upload_size":   size,
		"upload_files":  files,
		"uploaded_at":   now,
	}).Error; err != nil {
		return nil, err
	}
	// Only the latest upload is kept, unless a copy of the service in another
	// level still builds from the one before.
	if prevDigest != "" && (prevDigest != digest || prevRepo != repo) {
		var others int64
		s.db.WithContext(ctx).Model(&db.BuildConfig{}).
			Where("upload_repo = ? AND upload_digest = ? AND id <> ?", prevRepo, prevDigest, bc.ID).Count(&others)
		if others == 0 {
			reg.deleteTag(prevRepo, uploadTag(prevDigest))
		}
	}
	return &UploadedSource{Digest: digest, Name: name, Size: size, Files: files, UploadedAt: now}, nil
}

// buildConfigForUpload is the service's build config, made when it has none.
func (s *DeploymentService) buildConfigForUpload(ctx context.Context, svc *db.Service) (*db.BuildConfig, error) {
	var bc db.BuildConfig
	err := s.db.WithContext(ctx).Where("service_id = ?", svc.ID).First(&bc).Error
	if err == nil {
		return &bc, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	token, err := generateDeployToken()
	if err != nil {
		return nil, err
	}
	bc = db.BuildConfig{
		ServiceID:      svc.ID,
		Builder:        db.BuilderRailpack,
		Branch:         "main",
		DockerfilePath: "Dockerfile",
		DeployToken:    db.EncryptedString(token),
		// As a service made on its own from Git: its last images kept for
		// rollback, not every image it builds.
		RollbackEnabled: svc.StackID == nil,
		ImageRetention:  db.DefaultImageRetention,
	}
	if err := s.db.WithContext(ctx).Create(&bc).Error; err != nil {
		return nil, err
	}
	return &bc, nil
}

// checkUploadArchive reads the archive through, and returns how many files it
// holds. It refuses anything that is not a gzipped tar, an entry that would
// land outside the folder, and a link pointing out of it; the builder refuses
// the same again before it unpacks.
func checkUploadArchive(r io.Reader) (int, error) {
	zr, err := gzip.NewReader(r)
	if err != nil {
		return 0, fmt.Errorf("%w: not a gzipped tar", ErrUploadInvalid)
	}
	defer zr.Close()
	tr := tar.NewReader(zr)
	files := 0
	var unpacked int64
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return 0, fmt.Errorf("%w: %v", ErrUploadInvalid, err)
		}
		if !insideFolder(hdr.Name) {
			return 0, fmt.Errorf("%w: %q is outside the folder", ErrUploadInvalid, hdr.Name)
		}
		switch hdr.Typeflag {
		case tar.TypeReg, tar.TypeRegA: //nolint:staticcheck // TypeRegA is what old tars write
			files++
			unpacked += hdr.Size
			if unpacked > maxUnpackedBytes {
				return 0, fmt.Errorf("%w: it unpacks to over %d GB", ErrUploadInvalid, maxUnpackedBytes>>30)
			}
		case tar.TypeDir:
		case tar.TypeSymlink, tar.TypeLink:
			target := hdr.Linkname
			if hdr.Typeflag == tar.TypeSymlink {
				target = path.Join(path.Dir(hdr.Name), hdr.Linkname)
			}
			if path.IsAbs(hdr.Linkname) || !insideFolder(target) {
				return 0, fmt.Errorf("%w: %q links outside the folder", ErrUploadInvalid, hdr.Name)
			}
		case tar.TypeXGlobalHeader, tar.TypeXHeader:
		default:
			return 0, fmt.Errorf("%w: %q is not a file or folder", ErrUploadInvalid, hdr.Name)
		}
	}
	if files == 0 {
		return 0, fmt.Errorf("%w: the folder is empty", ErrUploadInvalid)
	}
	return files, nil
}

// insideFolder says an archive path stays in the folder it unpacks into.
func insideFolder(name string) bool {
	if name == "" || strings.HasPrefix(name, "/") || strings.Contains(name, "\\") {
		return false
	}
	clean := path.Clean(name)
	return clean != ".." && !strings.HasPrefix(clean, "../")
}

// uploadName is the folder's name as shown: its last element, kept short.
func uploadName(name string) string {
	name = strings.TrimSpace(path.Base(strings.ReplaceAll(name, "\\", "/")))
	if name == "" || name == "." || name == "/" {
		return "folder"
	}
	if len(name) > 100 {
		name = name[:100]
	}
	return name
}

// uploadTag names an upload in its repository: its digest, shortened.
func uploadTag(digest string) string {
	return "src-" + strings.TrimPrefix(digest, "sha256:")[:12]
}

// uploadBlobURL is where the build fetches the folder from.
func uploadBlobURL(host, repo, digest string) string {
	return registryClient{host: host}.base() + "/v2/" + repo + "/blobs/" + digest
}

// ─── Registry ────────────────────────────────────────────────────────────────

// registryClient speaks the registry API with a password, as the built-in
// registry and most self-hosted ones take it.
type registryClient struct {
	host, user, pass string
}

func (c registryClient) base() string {
	scheme := "http"
	if strings.HasPrefix(c.host, "https://") {
		scheme = "https"
	}
	bare := strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(c.host, "https://"), "http://"), "/")
	return scheme + "://" + bare
}

func (c registryClient) do(method, u string, body io.Reader, size int64, contentType string) (*http.Response, error) {
	req, err := http.NewRequest(method, u, body)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.ContentLength = size
		req.Header.Set("Content-Type", contentType)
	}
	if c.user != "" {
		req.SetBasicAuth(c.user, c.pass)
	}
	return (&http.Client{Timeout: 5 * time.Minute}).Do(req)
}

// pushSource stores the archive as the one layer of an artifact tagged tag.
func (c registryClient) pushSource(repo, tag, name string, archive io.Reader, digest string, size int64) error {
	config := []byte("{}")
	configDigest := fmt.Sprintf("sha256:%x", sha256.Sum256(config))
	if err := c.pushBlob(repo, bytes.NewReader(config), configDigest, int64(len(config))); err != nil {
		return err
	}
	if err := c.pushBlob(repo, archive, digest, size); err != nil {
		return err
	}
	manifest, _ := json.Marshal(map[string]any{
		"schemaVersion": 2,
		"mediaType":     ociManifestType,
		"config":        map[string]any{"mediaType": uploadConfigType, "digest": configDigest, "size": len(config)},
		"layers": []map[string]any{{
			"mediaType":   ociLayerType,
			"digest":      digest,
			"size":        size,
			"annotations": map[string]string{"org.opencontainers.image.title": name},
		}},
	})
	resp, err := c.do(http.MethodPut, fmt.Sprintf("%s/v2/%s/manifests/%s", c.base(), repo, tag),
		bytes.NewReader(manifest), int64(len(manifest)), ociManifestType)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return registryError("tag the folder", resp)
	}
	return nil
}

// pushBlob uploads a blob in one request, unless the registry has it already.
func (c registryClient) pushBlob(repo string, blob io.Reader, digest string, size int64) error {
	resp, err := c.do(http.MethodHead, fmt.Sprintf("%s/v2/%s/blobs/%s", c.base(), repo, digest), nil, 0, "")
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		return nil
	}
	resp, err = c.do(http.MethodPost, fmt.Sprintf("%s/v2/%s/blobs/uploads/", c.base(), repo), nil, 0, "")
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		return registryError("start the upload", resp)
	}
	loc, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		return fmt.Errorf("registry gave no upload location: %w", err)
	}
	baseURL, _ := url.Parse(c.base() + "/")
	put := baseURL.ResolveReference(loc)
	q := put.Query()
	q.Set("digest", digest)
	put.RawQuery = q.Encode()
	resp, err = c.do(http.MethodPut, put.String(), blob, size, "application/octet-stream")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return registryError("upload the folder", resp)
	}
	return nil
}

// deleteTag removes an upload's manifest; the registry's garbage collection
// then removes its blob. Best effort: a tag left behind costs only disk.
func (c registryClient) deleteTag(repo, tag string) {
	image := strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(c.host, "https://"), "http://"), "/") + "/" + repo + ":" + tag
	deleteRegistryImage(c.host, c.user, c.pass, image)
}

func registryError(what string, resp *http.Response) error {
	msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("could not %s: the registry wants a token sign-in, which folder uploads do not do yet; use the built-in registry", what)
	}
	return fmt.Errorf("could not %s: %s %s", what, resp.Status, strings.TrimSpace(string(msg)))
}

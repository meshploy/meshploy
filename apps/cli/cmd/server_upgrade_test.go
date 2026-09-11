package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// upgradeFixture is an install directory plus a release to upgrade it to, with
// every external command stubbed and recorded.
type upgradeFixture struct {
	live       string
	compose    []composeCall
	tags       [][]string
	ctl        [][]string // systemctl calls
	pullEnv    string     // the .env the image pull saw
	composeErr func(n int, args []string) error
	verify     []error // results of successive verifyStack calls, then nil
	verified   int
	fetchErr   error
}

type composeCall struct {
	dir  string
	args []string
}

const upgradeEnv = "DOMAIN=example.com\nPUBLIC_IP=203.0.113.7\nMESH_IP=100.64.0.1\nMESHPLOY_CHANNEL=latest\nPOSTGRES_PASSWORD=secret\n"

var liveFiles = map[string]string{
	"docker-compose.yml":           "old compose\n",
	"coredns/Corefile":             "old corefile ${DOMAIN}\n",
	"caddy/Caddyfile":              "old caddy\n",
	"caddy/Caddyfile.ondemand":     "old ondemand\n",
	"headscale/data/db.sqlite":     "mesh state\n",
	"headscale/config/config.yaml": "rendered headscale config\n",
	"coredns/zones/example.com":    "zone\n",
}

var releaseFiles = map[string]string{
	"docker-compose.yml":       "new compose\n",
	"coredns/Corefile":         "new corefile ${DOMAIN}\n",
	"caddy/Caddyfile":          "new caddy\n",
	"caddy/Caddyfile.ondemand": "new ondemand\n",
	"added-in-release.txt":     "added\n",
	// A tarball that slipped past tar's excludes must still not reach these.
	".env":                      "CLOBBERED=1\n",
	"headscale/data/db.sqlite":  "clobbered\n",
	"coredns/zones/example.com": "clobbered\n",
}

func newUpgradeFixture(t *testing.T) *upgradeFixture {
	t.Helper()
	fx := &upgradeFixture{live: t.TempDir()}

	origDir, origCfg := meshployInstDir, loadedCfg
	origRef, origFetch, origCompose := upgradeRefFor, fetchDeploy, composeExec
	origOut, origExec, origVerify, origLook := runtimeOutput, runtimeExec, verifyStack, lookPath
	origUpgrade, origUnits, origCtl := upgradeDir, systemdUnitDir, systemctl
	t.Cleanup(func() {
		meshployInstDir, loadedCfg = origDir, origCfg
		upgradeRefFor, fetchDeploy, composeExec = origRef, origFetch, origCompose
		runtimeOutput, runtimeExec, verifyStack, lookPath = origOut, origExec, origVerify, origLook
		upgradeDir, systemdUnitDir, systemctl = origUpgrade, origUnits, origCtl
	})
	meshployInstDir = fx.live
	loadedCfg = nil // no licence lookup against a real API
	// Docker unless a test says otherwise, whatever this machine has on PATH.
	lookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	upgradeDir = filepath.Join(t.TempDir(), "upgrade")
	systemdUnitDir = t.TempDir()
	systemctl = func(args ...string) error {
		fx.ctl = append(fx.ctl, args)
		return nil
	}

	writeTestFile(t, filepath.Join(fx.live, ".env"), upgradeEnv)
	if err := os.Chmod(filepath.Join(fx.live, ".env"), 0o600); err != nil {
		t.Fatal(err)
	}
	for rel, body := range liveFiles {
		writeTestFile(t, filepath.Join(fx.live, rel), body)
	}

	upgradeRefFor = func(string, bool) (string, error) { return "v9.9.9", nil }
	fetchDeploy = func(_, _, dest string) error {
		if fx.fetchErr != nil {
			return fx.fetchErr
		}
		for rel, body := range releaseFiles {
			writeTestFile(t, filepath.Join(dest, rel), body)
		}
		return nil
	}
	composeExec = func(dir, _ string, args ...string) error {
		fx.compose = append(fx.compose, composeCall{dir, args})
		if args[0] == "pull" {
			b, _ := os.ReadFile(filepath.Join(dir, ".env"))
			fx.pullEnv = string(b)
		}
		if fx.composeErr != nil {
			return fx.composeErr(len(fx.compose), args)
		}
		return nil
	}
	runtimeOutput = func(_, _ string, args ...string) ([]byte, error) {
		if args[0] == "compose" {
			return []byte("c1\nc2\n"), nil
		}
		return []byte("ghcr.io/meshploy/api:latest|sha256:old-api\npostgres:17-alpine|sha256:old-pg\n"), nil
	}
	runtimeExec = func(_ string, args ...string) error {
		fx.tags = append(fx.tags, args)
		return nil
	}
	verifyStack = func(context.Context) error {
		fx.verified++
		if fx.verified <= len(fx.verify) {
			return fx.verify[fx.verified-1]
		}
		return nil
	}
	return fx
}

func (fx *upgradeFixture) file(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(fx.live, rel))
	if errors.Is(err, os.ErrNotExist) {
		return "<missing>"
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func (fx *upgradeFixture) calls(sub string) int {
	n := 0
	for _, c := range fx.compose {
		if strings.Join(c.args, " ") == sub {
			n++
		}
	}
	return n
}

// The protected files hold rendered values and runtime state; no outcome of an
// upgrade may change them.
func (fx *upgradeFixture) assertProtectedUntouched(t *testing.T) {
	t.Helper()
	for _, rel := range []string{"headscale/data/db.sqlite", "headscale/config/config.yaml", "coredns/zones/example.com"} {
		if got := fx.file(t, rel); got != liveFiles[rel] {
			t.Errorf("protected %s changed to %q", rel, got)
		}
	}
	if strings.Contains(fx.file(t, ".env"), "CLOBBERED") {
		t.Error(".env was overwritten by the release")
	}
}

func (fx *upgradeFixture) assertPreviousVersion(t *testing.T) {
	t.Helper()
	for _, rel := range []string{"docker-compose.yml", "coredns/Corefile", "caddy/Caddyfile"} {
		if got := fx.file(t, rel); got != liveFiles[rel] {
			t.Errorf("%s = %q, want the pre-upgrade %q", rel, got, liveFiles[rel])
		}
	}
	if got := fx.file(t, "added-in-release.txt"); got != "<missing>" {
		t.Errorf("a file the release added was left behind: %q", got)
	}
	if got := fx.file(t, ".env"); got != upgradeEnv {
		t.Errorf(".env = %q, want it as it was", got)
	}
	fx.assertProtectedUntouched(t)
}

func TestServerUpgradeInstallsTheRelease(t *testing.T) {
	fx := newUpgradeFixture(t)

	if err := serverUpgrade(context.Background(), serverUpgradeOptions{}); err != nil {
		t.Fatal(err)
	}

	if got := fx.file(t, "docker-compose.yml"); got != "new compose\n" {
		t.Errorf("compose file = %q", got)
	}
	if got := fx.file(t, "coredns/Corefile"); got != "new corefile example.com\n" {
		t.Errorf("Corefile = %q, want the release's, rendered", got)
	}
	if got := fx.file(t, "added-in-release.txt"); got != "added\n" {
		t.Errorf("new file = %q", got)
	}
	fx.assertProtectedUntouched(t)
	if fi, _ := os.Stat(filepath.Join(fx.live, ".env")); fi.Mode().Perm() != 0o600 {
		t.Errorf(".env mode = %v", fi.Mode().Perm())
	}

	// Pulled from the staging directory, before anything restarted, and the
	// staging directory is gone afterwards.
	pull := fx.compose[0]
	if strings.Join(pull.args, " ") != "pull --quiet" || pull.dir == fx.live {
		t.Fatalf("first compose call = %+v, want a pull outside the live directory", pull)
	}
	if _, err := os.Stat(pull.dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("staging directory %s left behind", pull.dir)
	}
	if fx.calls("up -d --remove-orphans") != 1 || fx.calls("up -d --force-recreate caddy") != 1 {
		t.Errorf("compose calls = %+v", fx.compose)
	}
	if fx.verified != 1 || len(fx.tags) != 0 {
		t.Errorf("verified %d times, tags %v", fx.verified, fx.tags)
	}

	// What it replaced is kept, privately, for the operator.
	backup := filepath.Join(fx.live, upgradeBackupDirName)
	if b, _ := os.ReadFile(filepath.Join(backup, "docker-compose.yml")); string(b) != "old compose\n" {
		t.Errorf("backup compose = %q", b)
	}
	if fi, _ := os.Stat(backup); fi.Mode().Perm() != 0o700 {
		t.Errorf("backup dir mode = %v", fi.Mode().Perm())
	}
}

// The pull must resolve the tags the restarted stack will use, so it sees the
// channel written for this upgrade.
func TestServerUpgradePullsWithTheNewChannel(t *testing.T) {
	fx := newUpgradeFixture(t)
	if err := serverUpgrade(context.Background(), serverUpgradeOptions{edge: true}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(fx.pullEnv, "MESHPLOY_CHANNEL=main") || !strings.Contains(fx.pullEnv, "POSTGRES_PASSWORD=secret") {
		t.Errorf("the pull saw .env %q", fx.pullEnv)
	}
	if !strings.Contains(fx.file(t, ".env"), "MESHPLOY_CHANNEL=main") {
		t.Error("live .env not moved to the edge channel")
	}
}

func TestServerUpgradeFailedDownloadChangesNothing(t *testing.T) {
	fx := newUpgradeFixture(t)
	fx.fetchErr = errors.New("download tarball: exit status 22")

	if err := serverUpgrade(context.Background(), serverUpgradeOptions{edge: true}); err == nil {
		t.Fatal("want an error")
	}
	if len(fx.compose) != 0 {
		t.Errorf("ran compose after a failed download: %+v", fx.compose)
	}
	fx.assertPreviousVersion(t)
}

// The case this exists for: a pull that fails used to leave the new
// configuration on disk under the old containers.
func TestServerUpgradeFailedPullChangesNothing(t *testing.T) {
	fx := newUpgradeFixture(t)
	fx.composeErr = func(_ int, args []string) error {
		if args[0] == "pull" {
			return errors.New("exit status 1")
		}
		return nil
	}

	err := serverUpgrade(context.Background(), serverUpgradeOptions{edge: true})
	if err == nil || !strings.Contains(err.Error(), "Nothing was restarted") {
		t.Fatalf("got %v", err)
	}
	if fx.calls("up -d --remove-orphans") != 0 {
		t.Error("restarted services after a failed pull")
	}
	fx.assertPreviousVersion(t)
	if len(fx.tags) != 0 {
		t.Errorf("retagged images that were never replaced: %v", fx.tags)
	}
}

func TestServerUpgradeRollsBackWhenTheStackDoesNotComeBack(t *testing.T) {
	fx := newUpgradeFixture(t)
	fx.verify = []error{errors.New("API: status degraded")} // then healthy on the old version

	err := serverUpgrade(context.Background(), serverUpgradeOptions{edge: true})
	if err == nil || !strings.Contains(err.Error(), "rolled back") {
		t.Fatalf("got %v", err)
	}
	fx.assertPreviousVersion(t)
	wantTags := [][]string{
		{"tag", "sha256:old-api", "ghcr.io/meshploy/api:latest"},
		{"tag", "sha256:old-pg", "postgres:17-alpine"},
	}
	if !reflect.DeepEqual(fx.tags, wantTags) {
		t.Errorf("tags = %v, want %v", fx.tags, wantTags)
	}
	last := fx.compose[len(fx.compose)-1]
	if strings.Join(last.args, " ") != "up -d --remove-orphans --force-recreate" || last.dir != fx.live {
		t.Errorf("last compose call = %+v, want a full recreate in the live directory", last)
	}
	if fx.verified != 2 {
		t.Errorf("verified %d times, want 2 (after the upgrade and after the rollback)", fx.verified)
	}
}

func TestServerUpgradeRollsBackWhenTheRestartFails(t *testing.T) {
	fx := newUpgradeFixture(t)
	fx.composeErr = func(_ int, args []string) error {
		if strings.Join(args, " ") == "up -d --remove-orphans" {
			return errors.New("exit status 1")
		}
		return nil
	}

	err := serverUpgrade(context.Background(), serverUpgradeOptions{})
	if err == nil || !strings.Contains(err.Error(), "rolled back") {
		t.Fatalf("got %v", err)
	}
	fx.assertPreviousVersion(t)
	if fx.calls("up -d --remove-orphans --force-recreate") != 1 {
		t.Errorf("compose calls = %+v", fx.compose)
	}
}

func TestServerUpgradeNoRollbackLeavesTheFailureInPlace(t *testing.T) {
	fx := newUpgradeFixture(t)
	fx.verify = []error{errors.New("Caddy: connection refused")}

	err := serverUpgrade(context.Background(), serverUpgradeOptions{noRollback: true})
	if err == nil || !strings.Contains(err.Error(), "--no-rollback") {
		t.Fatalf("got %v", err)
	}
	if got := fx.file(t, "docker-compose.yml"); got != "new compose\n" {
		t.Errorf("compose file = %q, want the new one left in place", got)
	}
	if len(fx.tags) != 0 || fx.calls("up -d --remove-orphans --force-recreate") != 0 {
		t.Errorf("rolled back anyway: tags %v, compose %+v", fx.tags, fx.compose)
	}
}

func TestServerUpgradeReportsARollbackThatDidNotRecover(t *testing.T) {
	fx := newUpgradeFixture(t)
	fx.verify = []error{errors.New("API: status degraded"), errors.New("API: status degraded")}

	err := serverUpgrade(context.Background(), serverUpgradeOptions{})
	if err == nil || !strings.Contains(err.Error(), "rollback did not complete") ||
		!strings.Contains(err.Error(), upgradeBackupDirName) {
		t.Fatalf("got %v", err)
	}
	// The files are still the previous version: only the stack is unhealthy.
	fx.assertPreviousVersion(t)
}

// --no-sync installs nothing, but still renders the Corefile and Caddyfile in
// place, so those must come back too.
func TestServerUpgradeNoSyncRollsBackRenderedFiles(t *testing.T) {
	fx := newUpgradeFixture(t)
	fx.verify = []error{errors.New("API: status degraded")}

	if err := serverUpgrade(context.Background(), serverUpgradeOptions{noSync: true}); err == nil {
		t.Fatal("want an error")
	}
	if fx.compose[0].dir != fx.live {
		t.Errorf("--no-sync pulled in %s, want the live directory", fx.compose[0].dir)
	}
	if got := fx.file(t, "coredns/Corefile"); got != liveFiles["coredns/Corefile"] {
		t.Errorf("Corefile = %q, want it as it was", got)
	}
	fx.assertProtectedUntouched(t)
}

// withEnterpriseImages makes the fixture an Enterprise install whose pulled
// images were built from builtFrom, and counts the label lookups.
func (fx *upgradeFixture) withEnterpriseImages(t *testing.T, builtFrom string) (env string, inspected *int) {
	t.Helper()
	env = upgradeEnv + "MESHPLOY_API_IMAGE=ghcr.io/meshploy/api-ee\nMESHPLOY_WEB_IMAGE=ghcr.io/meshploy/web-ee\n"
	writeTestFile(t, filepath.Join(fx.live, ".env"), env)
	n := 0
	stock := runtimeOutput
	runtimeOutput = func(dir, runtime string, args ...string) ([]byte, error) {
		if args[0] == "image" && args[1] == "inspect" {
			n++
			return []byte(builtFrom + "\n"), nil
		}
		return stock(dir, runtime, args...)
	}
	return env, &n
}

// The Enterprise build follows a Community release by minutes. Upgrading an
// Enterprise server inside that gap would run the previous Enterprise image
// under the new release, so it stops after the pull, with nothing restarted.
func TestServerUpgradeStopsForEnterpriseImagesFromAnotherRelease(t *testing.T) {
	fx := newUpgradeFixture(t)
	env, _ := fx.withEnterpriseImages(t, "v9.9.8")

	err := serverUpgrade(context.Background(), serverUpgradeOptions{})
	if err == nil || !strings.Contains(err.Error(), "Enterprise build for v9.9.9 is not published yet") {
		t.Fatalf("got %v", err)
	}
	if fx.calls("up -d --remove-orphans") != 0 {
		t.Error("restarted services on a mismatched Enterprise image")
	}
	if got := fx.file(t, ".env"); got != env {
		t.Errorf(".env = %q, want it as it was", got)
	}
	if got := fx.file(t, "docker-compose.yml"); got != liveFiles["docker-compose.yml"] {
		t.Errorf("compose file = %q, want it as it was", got)
	}
}

func TestServerUpgradeAcceptsEnterpriseImagesFromTheRelease(t *testing.T) {
	fx := newUpgradeFixture(t)
	_, inspected := fx.withEnterpriseImages(t, "v9.9.9")

	if err := serverUpgrade(context.Background(), serverUpgradeOptions{}); err != nil {
		t.Fatal(err)
	}
	if *inspected != 2 {
		t.Errorf("checked %d images, want the API and the console", *inspected)
	}

	// On edge the images are built from main.
	fx = newUpgradeFixture(t)
	fx.withEnterpriseImages(t, "main")
	upgradeRefFor = func(string, bool) (string, error) { return "main", nil }
	if err := serverUpgrade(context.Background(), serverUpgradeOptions{edge: true}); err != nil {
		t.Fatal(err)
	}
}

// A Community install has no Enterprise image to hold to a release.
func TestServerUpgradeLeavesCommunityImagesUnchecked(t *testing.T) {
	newUpgradeFixture(t)
	n := 0
	stock := runtimeOutput
	runtimeOutput = func(dir, runtime string, args ...string) ([]byte, error) {
		if args[0] == "image" {
			n++
		}
		return stock(dir, runtime, args...)
	}
	if err := serverUpgrade(context.Background(), serverUpgradeOptions{}); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("inspected %d images on a Community install", n)
	}
}

func TestRecordRunningImagesWithNothingRunning(t *testing.T) {
	newUpgradeFixture(t)
	runtimeOutput = func(_, _ string, _ ...string) ([]byte, error) { return []byte("\n"), nil }
	images, err := recordRunningImages("docker")
	if err != nil || images == nil || len(images) != 0 {
		t.Fatalf("got %v, %v", images, err)
	}
}

// The API's compose mounts need the updater's folders to exist, since podman
// will not create them, and an updater that is on must end up running the
// unit files of the CLI that just upgraded the server.
func TestServerUpgradePreparesTheUpdater(t *testing.T) {
	fx := newUpgradeFixture(t)
	if err := serverUpgrade(context.Background(), serverUpgradeOptions{}); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{upgradeInboxDir(), upgradeStateDir()} {
		if !fileExists(d) {
			t.Errorf("%s not created", d)
		}
	}
	if fileExists(filepath.Join(systemdUnitDir, upgradeServiceUnit)) || len(fx.ctl) != 0 {
		t.Errorf("touched systemd although the updater is off: %v", fx.ctl)
	}

	writeTestFile(t, filepath.Join(upgradeStateDir(), upgradeEnabledFile), "on\n")
	if err := serverUpgrade(context.Background(), serverUpgradeOptions{}); err != nil {
		t.Fatal(err)
	}
	if !fileExists(filepath.Join(systemdUnitDir, upgradeServiceUnit)) {
		t.Error("units not refreshed although the updater is on")
	}
	if !reflect.DeepEqual(fx.ctl, [][]string{{"daemon-reload"}}) {
		t.Errorf("systemctl calls = %v, want only a reload", fx.ctl)
	}
}

// ── waitForHealthyStack ──────────────────────────────────────────────────────

func TestWaitForHealthyStackAcceptsWhatTheGatewayAnswers(t *testing.T) {
	origInterval := upgradeHealthInterval
	upgradeHealthInterval = 10 * time.Millisecond
	t.Cleanup(func() { upgradeHealthInterval = origInterval })

	serve := func(h http.HandlerFunc) string {
		srv := httptest.NewServer(h)
		t.Cleanup(srv.Close)
		return srv.URL
	}
	api := serve(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, `{"status":"ok","db":"ok"}`) })
	console := serve(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "<html>") })
	proxy := serve(func(w http.ResponseWriter, _ *http.Request) { http.NotFound(w, nil) })
	var caddyHost string
	caddy := serve(func(w http.ResponseWriter, r *http.Request) {
		caddyHost = r.Host
		http.Redirect(w, r, "https://"+r.Host+"/", http.StatusPermanentRedirect)
	})

	checks := []stackCheck{
		{name: "API", url: api, api: true},
		{name: "console", url: console},
		{name: "proxy", url: proxy},
		{name: "Caddy", url: caddy, host: "console.example.com"},
	}
	if err := waitForHealthyStack(context.Background(), checks, time.Second); err != nil {
		t.Fatal(err)
	}
	if caddyHost != "console.example.com" {
		t.Errorf("Caddy was asked for %q", caddyHost)
	}

	// A service that does not answer (nothing listens on port 1), and an API
	// that is up but degraded.
	checks[2].url = "http://127.0.0.1:1/"
	checks[0].url = serve(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, `{"status":"degraded","db":"refused"}`) })

	err := waitForHealthyStack(context.Background(), checks, 50*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "proxy:") || !strings.Contains(err.Error(), "API: status degraded") ||
		strings.Contains(err.Error(), "console:") {
		t.Fatalf("got %v", err)
	}
}

func TestWaitForHealthyStackFailsOnServerErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()
	err := waitForHealthyStack(context.Background(), []stackCheck{{name: "console", url: srv.URL}}, 0)
	if err == nil || !strings.Contains(err.Error(), "console: HTTP 502") {
		t.Fatalf("got %v", err)
	}
}

// podmanRelease makes the fixture a Podman install whose release names images
// the way the real compose file does.
func (fx *upgradeFixture) podmanRelease(t *testing.T) {
	t.Helper()
	writeTestFile(t, filepath.Join(fx.live, ".env"), upgradeEnv+"CONTAINER_RUNTIME=podman\nMESHPLOY_API_IMAGE=ghcr.io/meshploy/api-ee\n")
	// The Enterprise image was built from the release being installed.
	out := runtimeOutput
	runtimeOutput = func(dir, runtime string, args ...string) ([]byte, error) {
		if args[0] == "image" {
			return []byte("v9.9.9\n"), nil
		}
		return out(dir, runtime, args...)
	}
	stock := fetchDeploy
	fetchDeploy = func(pat, ref, dest string) error {
		if err := stock(pat, ref, dest); err != nil {
			return err
		}
		writeTestFile(t, filepath.Join(dest, "docker-compose.yml"),
			"services:\n  postgres:\n    image: docker.io/library/postgres:17-alpine\n"+
				"  api:\n    image: ${MESHPLOY_API_IMAGE:-ghcr.io/meshploy/api}:${MESHPLOY_CHANNEL:-latest}\n"+
				"  web:\n    image: ${MESHPLOY_WEB_IMAGE:-ghcr.io/meshploy/web}:${MESHPLOY_CHANNEL:-latest}\n")
		return nil
	}
}

// podman-compose's pull takes no --quiet and reports success when it fails,
// and its up keeps containers on their old images. So on Podman each image is
// pulled with podman, resolved as compose resolves it, and every container is
// recreated.
func TestServerUpgradeOnPodmanPullsEachImageAndRecreates(t *testing.T) {
	fx := newUpgradeFixture(t)
	fx.podmanRelease(t)

	if err := serverUpgrade(context.Background(), serverUpgradeOptions{}); err != nil {
		t.Fatal(err)
	}
	wantPulls := [][]string{
		{"pull", "--quiet", "docker.io/library/postgres:17-alpine"},
		{"pull", "--quiet", "ghcr.io/meshploy/api-ee:latest"},
		{"pull", "--quiet", "ghcr.io/meshploy/web:latest"},
	}
	if !reflect.DeepEqual(fx.tags, wantPulls) {
		t.Errorf("podman ran %v, want %v", fx.tags, wantPulls)
	}
	if fx.calls("pull --quiet") != 0 {
		t.Error("pulled through compose on Podman")
	}
	if fx.calls("up -d --remove-orphans --force-recreate") != 1 {
		t.Errorf("compose calls = %+v, want a forced recreate", fx.compose)
	}
}

func TestServerUpgradeOnPodmanStopsWhenAPullFails(t *testing.T) {
	fx := newUpgradeFixture(t)
	fx.podmanRelease(t)
	runtimeExec = func(_ string, args ...string) error {
		if args[0] == "pull" && strings.Contains(args[len(args)-1], "api-ee") {
			return errors.New("unauthorized")
		}
		return nil
	}

	err := serverUpgrade(context.Background(), serverUpgradeOptions{})
	if err == nil || !strings.Contains(err.Error(), "pull ghcr.io/meshploy/api-ee:latest") {
		t.Fatalf("got %v, want the failed pull named", err)
	}
	if len(fx.compose) != 0 {
		t.Errorf("ran compose after a failed pull: %+v", fx.compose)
	}
	if got := fx.file(t, "docker-compose.yml"); got != liveFiles["docker-compose.yml"] {
		t.Errorf("compose file = %q, want it as it was", got)
	}
}

// Podman will not resolve a short name like postgres:17-alpine without a
// terminal on Fedora, nor at all on Debian and Ubuntu, so every image the stack
// runs names its registry.
func TestComposeImagesAreFullyQualified(t *testing.T) {
	withEnvFile(t, "MESHPLOY_CHANNEL=main\n")
	images, err := composeImages(filepath.Join("..", "..", "..", "deploy", "docker-compose.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(images) < 8 {
		t.Fatalf("found %d images: %v", len(images), images)
	}
	for _, image := range images {
		host, _, _ := strings.Cut(image, "/")
		if !strings.Contains(host, ".") {
			t.Errorf("%s does not name its registry", image)
		}
		if strings.Contains(image, "${") {
			t.Errorf("%s was not resolved", image)
		}
	}
}

// .env holds every secret of the install. Earlier installers left it readable
// by every user on the host; an upgrade makes it private.
func TestServerUpgradeMakesEnvPrivate(t *testing.T) {
	fx := newUpgradeFixture(t)
	envPath := filepath.Join(fx.live, ".env")
	if err := os.Chmod(envPath, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := serverUpgrade(context.Background(), serverUpgradeOptions{}); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(envPath)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf(".env mode = %v, want 0600", fi.Mode().Perm())
	}
}

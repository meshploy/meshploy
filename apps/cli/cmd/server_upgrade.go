package cmd

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const meshployRepo = "meshploy/meshploy"

// meshployInstDir is where get.sh lays down deploy/. A var, not a const, so
// tests can point the .env helpers at a temporary directory.
var meshployInstDir = "/opt/meshploy"

// upgradeBackupDirName holds what the last upgrade replaced, inside the install
// directory so uninstalling removes it with the rest. Mode 0700, because it
// holds a copy of .env.
const upgradeBackupDirName = ".upgrade-previous"

// protectedUpgradePaths are never written by an upgrade: they hold values
// rendered at install time, or runtime state. The tarball extraction already
// excludes them; installStaged checks again so that guarantee does not rest on
// tar alone.
var protectedUpgradePaths = []string{".env", "coredns/zones", "headscale/config/config.yaml", "headscale/data"}

// Seams replaced by tests.
var (
	upgradeRefFor = resolveUpgradeRef
	fetchDeploy   = downloadDeployTarball
	composeExec   = func(dir, runtime string, args ...string) error {
		c := exec.Command(runtime, append([]string{"compose"}, args...)...)
		c.Dir = dir
		c.Stdout, c.Stderr = os.Stdout, os.Stderr
		return c.Run()
	}
	runtimeOutput = func(dir, runtime string, args ...string) ([]byte, error) {
		c := exec.Command(runtime, args...)
		c.Dir = dir
		return c.Output()
	}
	runtimeExec = func(runtime string, args ...string) error {
		return sysCmd(runtime, args...)
	}
	verifyStack = func(ctx context.Context) error {
		return waitForHealthyStack(ctx, stackChecks(), upgradeHealthTimeout)
	}
)

var serverUpgradeCmd = &cobra.Command{
	Use:   "server-upgrade",
	Short: "Sync deploy configs and pull latest images on this gateway server",
	Long: `Upgrades this gateway: downloads the deploy/ configuration for the release
and pulls its images, then installs the configuration, restarts the services
and checks that they answer.

Nothing changes until the download and the pull have both succeeded, so a
failure there leaves the server as it was. If the restart or the check fails,
the previous configuration and images are put back and the services restarted
on them; --no-rollback leaves the failed state in place for inspection instead.
The files an upgrade replaces are kept in /opt/meshploy/.upgrade-previous until
the next one. Database migrations the new version ran are not undone.

Must be run as root on the gateway server (sudo meshploy server-upgrade).

By default pulls the latest stable release. Use --edge to follow the main branch.
Use --no-sync to skip the config download (e.g. when CI has already rsync'd configs).

Enterprise
  --ee switches this install to the Enterprise API image. Activate the licence
  in the console first (Settings → Licence); this command then reads which
  image that licence grants and points the stack at it.

  Nothing else changes: the licence is already stored server-side and every
  feature gate reads it at runtime, so the image is the whole difference
  between a Community and an Enterprise install.

  Without --ee, a licensed install running the stock image is told about it and
  left alone — a routine upgrade should not silently change which product is
  running.`,
	RunE: runServerUpgrade,
}

type serverUpgradeOptions struct {
	pat        string
	edge       bool
	noSync     bool
	noRollback bool
	ee         bool
	eeImage    string
}

func runServerUpgrade(cmd *cobra.Command, _ []string) error {
	var o serverUpgradeOptions
	o.pat, _ = cmd.Flags().GetString("token")
	if o.pat == "" {
		o.pat = os.Getenv("GITHUB_PAT")
	}
	o.edge, _ = cmd.Flags().GetBool("edge")
	o.noSync, _ = cmd.Flags().GetBool("no-sync")
	o.noRollback, _ = cmd.Flags().GetBool("no-rollback")
	o.ee, _ = cmd.Flags().GetBool("ee")
	o.eeImage, _ = cmd.Flags().GetString("ee-image")

	if !o.noSync && os.Getuid() != 0 {
		return fmt.Errorf("must be run as root — try: sudo meshploy server-upgrade")
	}
	return serverUpgrade(cmd.Context(), o)
}

// serverUpgrade applies a release in an order that can be undone.
//
// Everything that can fail without touching the running stack goes first: the
// download into a private staging directory and the image pull. Only then are
// the live files replaced and the services restarted, with every replaced file
// saved and every running image recorded, so a restart that fails or a stack
// that does not come back can be put back as it was.
func serverUpgrade(ctx context.Context, o serverUpgradeOptions) error {
	runtime := detectContainerRuntime()

	// What Caddy is serving now, to tell afterwards whether it must be recreated.
	// Absent on a broken install; only ever compared, never required.
	caddyfile := filepath.Join(meshployInstDir, "caddy", "Caddyfile")
	caddyBefore, _ := os.ReadFile(caddyfile)

	// Download into a private directory first. Nothing live has changed yet,
	// so a failed download leaves the server exactly as it was.
	var staged string
	if !o.noSync {
		ref, err := upgradeRefFor(o.pat, o.edge)
		if err != nil {
			return err
		}
		if o.edge {
			fmt.Println("Upgrading from edge (main)…")
		} else {
			fmt.Printf("Upgrading to stable release %s…\n", ref)
		}
		// MkdirTemp creates it 0700: it gets a copy of .env for the pull.
		staged, err = os.MkdirTemp("", "meshploy-upgrade-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(staged)

		fmt.Println("Downloading deploy configs…")
		if err := fetchDeploy(o.pat, ref, staged); err != nil {
			return err
		}
	}

	// From here on, every change is recorded so a failure can put it back.
	snap, err := newConfigSnapshot(filepath.Join(meshployInstDir, upgradeBackupDirName))
	if err != nil {
		return fmt.Errorf("prepare %s: %w", upgradeBackupDirName, err)
	}
	if err := snap.save(meshployInstDir, ".env"); err != nil {
		return fmt.Errorf("save .env: %w", err)
	}
	images, err := recordRunningImages(runtime)
	if err != nil {
		fmt.Printf("warning: could not record the running images, so a rollback would keep the new ones: %v\n", err)
	}

	// Sync MESHPLOY_CHANNEL in .env so image pulls match the chosen channel.
	channel := "latest"
	if o.edge {
		channel = "main"
	}
	if err := syncEnvChannel(channel); err != nil {
		fmt.Printf("warning: could not update MESHPLOY_CHANNEL in .env: %v\n", err)
	}

	// Enterprise image selection. Explicit --ee switches; otherwise a licensed
	// install running the stock image just gets told, because a routine upgrade
	// should not silently change which product is running.
	scope := entitledRegistryScope()
	if o.ee {
		eeImage := o.eeImage
		if eeImage == "" {
			eeImage = scope
		}
		if err := applyEEImage(runtime, eeImage, o.pat); err != nil {
			return putBackBeforeRestart(snap, err)
		}
	} else {
		eeNotice(currentAPIImage(), scope)
	}

	// Pull before the live configuration changes. From the staging directory,
	// with a copy of the .env just written, so the pull resolves the tags the
	// restarted stack will use; --no-sync has its configuration in place
	// already.
	pullDir := meshployInstDir
	if staged != "" {
		if err := copyFileKeepMode(filepath.Join(meshployInstDir, ".env"), filepath.Join(staged, ".env")); err != nil {
			return putBackBeforeRestart(snap, err)
		}
		pullDir = staged
	}
	fmt.Println("Pulling images…")
	if err := composeExec(pullDir, runtime, "pull", "--quiet"); err != nil {
		return putBackBeforeRestart(snap, fmt.Errorf("compose pull: %w", err))
	}

	fail := func(err error) error {
		return rollBack(ctx, runtime, snap, images, err, o.noRollback)
	}

	if staged != "" {
		fmt.Println("Installing deploy configs…")
		if err := installStaged(staged, meshployInstDir, snap); err != nil {
			return fail(fmt.Errorf("install deploy configs: %w", err))
		}
		fmt.Println("✔  Deploy configs synced")
	}

	// Rendered in place below. Saved first so a --no-sync upgrade, which
	// installed nothing, can put them back too; a no-op when the release
	// already replaced them.
	for _, rel := range []string{"coredns/Corefile", "caddy/Caddyfile"} {
		if err := snap.save(meshployInstDir, rel); err != nil {
			return fail(err)
		}
	}

	// Substitute ${DOMAIN}, ${PUBLIC_IP}, ${MESH_IP} in the Corefile using .env values.
	fmt.Println("Configuring Corefile…")
	if err := substituteCorefile(); err != nil {
		return fail(fmt.Errorf("corefile substitution: %w", err))
	}
	fmt.Println("✔  Corefile configured")

	if err := applyDNSModeCaddyfile(readEnvVar("DNS_MODE")); err != nil {
		return fail(fmt.Errorf("caddyfile: %w", err))
	}
	caddyAfter, _ := os.ReadFile(caddyfile)

	fmt.Println("Restarting services…")
	if err := composeRun(runtime, "up", "-d", "--remove-orphans"); err != nil {
		return fail(fmt.Errorf("compose up: %w", err))
	}

	// up -d recreates a container only when its compose definition changes, not
	// when a file it mounts does, so a new Caddyfile would sit on disk unserved
	// until Caddy next happened to restart. Certificates live in the caddy_data
	// volume and survive the recreate.
	if !bytes.Equal(caddyBefore, caddyAfter) {
		fmt.Println("Recreating Caddy to load the new Caddyfile…")
		if err := composeRun(runtime, "up", "-d", "--force-recreate", "caddy"); err != nil {
			return fail(fmt.Errorf("recreate caddy: %w", err))
		}
	}

	fmt.Println("Checking that the services answer…")
	if err := verifyStack(ctx); err != nil {
		return fail(err)
	}

	fmt.Println("✔  Server upgraded successfully")
	return nil
}

// putBackBeforeRestart undoes the configuration changes after a failure that
// came before any service restarted, so the running stack never saw them.
func putBackBeforeRestart(snap *configSnapshot, cause error) error {
	if err := snap.restore(meshployInstDir); err != nil {
		return fmt.Errorf("%w\nPutting the configuration back failed too: %v\nThe previous files are in %s", cause, err, snap.dir)
	}
	return fmt.Errorf("%w\nNothing was restarted, and the configuration is as it was", cause)
}

// rollBack returns the stack to the version it ran before the upgrade: the
// saved files, the recorded images, and a restart on them.
func rollBack(ctx context.Context, runtime string, snap *configSnapshot, images []imageRef, cause error, disabled bool) error {
	if disabled {
		return fmt.Errorf("%w\nLeft as it is (--no-rollback). The files the upgrade replaced are in %s", cause, snap.dir)
	}
	fmt.Printf("\n✘  %v\nRolling back to the previous version…\n", cause)

	var errs []error
	if err := snap.restore(meshployInstDir); err != nil {
		errs = append(errs, err)
	}
	if images == nil {
		fmt.Println("warning: the running images were not recorded, so the new ones stay")
	}
	for _, im := range images {
		if err := runtimeExec(runtime, "tag", im.id, im.ref); err != nil {
			errs = append(errs, fmt.Errorf("retag %s: %w", im.ref, err))
		}
	}
	// Recreated in full rather than left to up -d: Caddy and CoreDNS mount
	// single files, and only a new container sees the restored ones.
	if err := composeRun(runtime, "up", "-d", "--remove-orphans", "--force-recreate"); err != nil {
		errs = append(errs, fmt.Errorf("compose up: %w", err))
	}
	if len(errs) == 0 {
		if err := verifyStack(ctx); err != nil {
			errs = append(errs, fmt.Errorf("still not healthy on the previous version: %w", err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("upgrade failed: %v\nThe rollback did not complete either: %v\nThe files the upgrade replaced are in %s", cause, errors.Join(errs...), snap.dir)
	}
	fmt.Println("✔  Rolled back: the server is running the previous version again")
	fmt.Println("   Database migrations the new version ran are not undone.")
	return fmt.Errorf("upgrade failed and was rolled back: %w", cause)
}

// configSnapshot records the live files an upgrade replaces, so they can be put
// back. Copies go in dir; files the upgrade adds are remembered so a rollback
// removes them.
type configSnapshot struct {
	dir     string
	saved   []string
	created []string
	seen    map[string]bool
}

func newConfigSnapshot(dir string) (*configSnapshot, error) {
	if err := os.RemoveAll(dir); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return &configSnapshot{dir: dir, seen: map[string]bool{}}, nil
}

// save records the live file at rel before its first change. Later calls for
// the same path keep the first copy, which is the pre-upgrade one.
func (s *configSnapshot) save(live, rel string) error {
	if s.seen[rel] {
		return nil
	}
	src := filepath.Join(live, rel)
	fi, err := os.Lstat(src)
	if errors.Is(err, fs.ErrNotExist) {
		s.seen[rel] = true
		s.created = append(s.created, rel)
		return nil
	}
	if err != nil {
		return err
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", src)
	}
	dst := filepath.Join(s.dir, rel)
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	if err := copyFileKeepMode(src, dst); err != nil {
		return err
	}
	s.seen[rel] = true
	s.saved = append(s.saved, rel)
	return nil
}

// restore puts every saved file back and removes the ones the upgrade added.
// It carries on past a failure so one bad file does not strand the rest.
func (s *configSnapshot) restore(live string) error {
	var errs []error
	for _, rel := range s.saved {
		if err := copyFileKeepMode(filepath.Join(s.dir, rel), filepath.Join(live, rel)); err != nil {
			errs = append(errs, fmt.Errorf("restore %s: %w", rel, err))
		}
	}
	for _, rel := range s.created {
		if err := os.Remove(filepath.Join(live, rel)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, fmt.Errorf("remove %s: %w", rel, err))
		}
	}
	return errors.Join(errs...)
}

// installStaged copies the release from staged over the live directory,
// saving each file it replaces into snap first. Files are replaced, not
// rewritten in place, which is what the tar extraction this replaces did.
func installStaged(staged, live string, snap *configSnapshot) error {
	return filepath.WalkDir(staged, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(staged, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		if isProtectedUpgradePath(rel) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		dst := filepath.Join(live, rel)
		if d.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("the release contains %s, which is not a regular file", rel)
		}
		if err := snap.save(live, rel); err != nil {
			return err
		}
		return copyFileKeepMode(path, dst)
	})
}

func isProtectedUpgradePath(rel string) bool {
	for _, p := range protectedUpgradePaths {
		if rel == p || strings.HasPrefix(rel, p+"/") {
			return true
		}
	}
	return false
}

// copyFileKeepMode replaces dst with a copy of src, keeping src's permissions.
func copyFileKeepMode(src, dst string) error {
	fi, err := os.Stat(src)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return writeFileAtomic(dst, data, fi.Mode().Perm())
}

// imageRef is an image a running service uses: the name compose asked for,
// and the image that name pointed at when the upgrade started.
type imageRef struct{ ref, id string }

// recordRunningImages notes which image every running service uses, so a
// rollback can point each name back at it after the pull has moved it on.
func recordRunningImages(runtime string) ([]imageRef, error) {
	out, err := runtimeOutput(meshployInstDir, runtime, "compose", "ps", "-q")
	if err != nil {
		return nil, fmt.Errorf("compose ps: %w", err)
	}
	ids := strings.Fields(string(out))
	if len(ids) == 0 {
		return []imageRef{}, nil
	}
	args := append([]string{"inspect", "--format", "{{.Config.Image}}|{{.Image}}"}, ids...)
	out, err = runtimeOutput(meshployInstDir, runtime, args...)
	if err != nil {
		return nil, fmt.Errorf("inspect: %w", err)
	}
	images := []imageRef{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		ref, id, ok := strings.Cut(strings.TrimSpace(line), "|")
		if ok && ref != "" && id != "" {
			images = append(images, imageRef{ref: ref, id: id})
		}
	}
	return images, nil
}

// stackCheck is one service to probe after a restart.
type stackCheck struct {
	name string
	url  string
	host string // Host header, for a service that routes on it
	api  bool   // must report {"status":"ok"}; the others only have to answer
}

// stackChecks are the ports docker-compose publishes on the gateway itself. An
// HTTP answer from the console, proxy or Caddy means the process is up with its
// configuration loaded: the proxy answers 404 for a host it has no route for,
// and Caddy 308 to HTTPS. A crashed container does not answer at all.
func stackChecks() []stackCheck {
	host := ""
	if d := readEnvVar("DOMAIN"); d != "" {
		host = "console." + d
	}
	return []stackCheck{
		{name: "API", url: localAPI + "/health", api: true},
		{name: "console", url: "http://127.0.0.1:5173/"},
		{name: "proxy", url: "http://127.0.0.1:8081/"},
		{name: "Caddy", url: "http://127.0.0.1:80/", host: host},
	}
}

// waitForHealthyStack polls every check until all pass or timeout runs out.
// Connection errors are expected while containers start, so only the state at
// the deadline is reported.
func waitForHealthyStack(ctx context.Context, checks []stackCheck, timeout time.Duration) error {
	c := &http.Client{
		Timeout: 5 * time.Second,
		// A redirect is an answer; following Caddy's to HTTPS would test TLS,
		// which is not what this is checking.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	deadline := time.Now().Add(timeout)
	pending := checks
	for {
		var still []stackCheck
		var why []string
		for _, ch := range pending {
			if reason := probeStackCheck(ctx, c, ch); reason != "" {
				still = append(still, ch)
				why = append(why, ch.name+": "+reason)
			}
		}
		if len(still) == 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("not answering %s after the restart: %s", timeout, strings.Join(why, "; "))
		}
		pending = still
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(upgradeHealthInterval):
		}
	}
}

// probeStackCheck returns "" when the check passes, and otherwise why not.
func probeStackCheck(ctx context.Context, c *http.Client, ch stackCheck) string {
	if ch.api {
		return apiHealth(ctx, c, ch.url)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ch.url, nil)
	if err != nil {
		return err.Error()
	}
	if ch.host != "" {
		req.Host = ch.host
	}
	resp, err := c.Do(req)
	if err != nil {
		return err.Error()
	}
	resp.Body.Close()
	if resp.StatusCode >= 500 {
		return fmt.Sprintf("HTTP %d", resp.StatusCode)
	}
	return ""
}

// substituteCorefile reads DOMAIN, PUBLIC_IP, MESH_IP from .env and replaces
// the placeholder variables in the Corefile template in-place.
func substituteCorefile() error {
	envPath := filepath.Join(meshployInstDir, ".env")
	corefilePath := filepath.Join(meshployInstDir, "coredns", "Corefile")

	vars, err := parseEnvFile(envPath, "DOMAIN", "PUBLIC_IP", "MESH_IP")
	if err != nil {
		return err
	}

	content, err := os.ReadFile(corefilePath)
	if err != nil {
		return err
	}

	result := string(content)
	for k, v := range vars {
		result = strings.ReplaceAll(result, "${"+k+"}", v)
	}

	return os.WriteFile(corefilePath, []byte(result), 0644)
}

// parseEnvFile reads KEY=VALUE lines from an env file and returns the requested keys.
func parseEnvFile(path string, keys ...string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	want := make(map[string]bool, len(keys))
	for _, k := range keys {
		want[k] = true
	}

	result := make(map[string]string, len(keys))
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok || !want[k] {
			continue
		}
		result[k] = v
	}

	for _, k := range keys {
		if _, ok := result[k]; !ok {
			return nil, fmt.Errorf("%s not found in %s", k, path)
		}
	}
	return result, scanner.Err()
}

func resolveUpgradeRef(pat string, edge bool) (string, error) {
	if edge {
		return "main", nil
	}
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", meshployRepo)
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	if pat != "" {
		req.Header.Set("Authorization", "token "+pat)
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch latest release: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("GitHub API %d: %s", resp.StatusCode, string(b))
	}
	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", fmt.Errorf("parse release: %w", err)
	}
	if release.TagName == "" {
		return "", fmt.Errorf("no release found")
	}
	return release.TagName, nil
}

// downloadDeployTarball extracts the release's deploy/ directory into dest.
func downloadDeployTarball(pat, ref, dest string) error {
	tarURL := fmt.Sprintf("https://api.github.com/repos/%s/tarball/%s", meshployRepo, ref)

	// Files already rendered with real values on disk — never overwrite.
	protected := []string{
		"*/deploy/.env",
		"*/deploy/coredns/zones",
		"*/deploy/headscale/config/config.yaml",
		"*/deploy/headscale/data",
	}

	curlArgs := []string{"-fsSL"}
	if pat != "" {
		curlArgs = append(curlArgs, "-H", "Authorization: token "+pat)
	}
	curlArgs = append(curlArgs, tarURL)

	tarArgs := []string{"-xz", "--strip-components=2", "-C", dest, "--wildcards", "*/deploy"}
	for _, p := range protected {
		tarArgs = append(tarArgs, "--exclude="+p)
	}

	curl := exec.Command("curl", curlArgs...)
	tar := exec.Command("tar", tarArgs...)
	tar.Stdout = os.Stdout
	tar.Stderr = os.Stderr

	pr, pw := io.Pipe()
	curl.Stdout = pw
	curl.Stderr = os.Stderr
	tar.Stdin = pr

	if err := curl.Start(); err != nil {
		return fmt.Errorf("start curl: %w", err)
	}
	if err := tar.Start(); err != nil {
		return fmt.Errorf("start tar: %w", err)
	}

	curlErr := curl.Wait()
	pw.Close()
	tarErr := tar.Wait()

	if curlErr != nil {
		return fmt.Errorf("download tarball: %w", curlErr)
	}
	if tarErr != nil {
		return fmt.Errorf("extract tarball: %w", tarErr)
	}
	return nil
}

// applyDNSModeCaddyfile puts the Caddyfile for the gateway's DNS mode at
// caddy/Caddyfile, the fixed path docker-compose mounts.
//
// The deploy tarball ships the NS-delegation config at that path and the
// on-demand one beside it. install.sh copies the on-demand one over when
// DNS_MODE=ondemand, but an upgrade unpacked the tarball and stopped there, so
// an on-demand gateway came back on the delegation config the next time Caddy
// started: DNS-01 through an NS delegation it does not have, so no new
// certificate and no renewal. DNS_MODE is read from .env because that is the
// one file an upgrade keeps.
//
// The rule mirrors install.sh, which runs in the same two situations: when
// caddy/Caddyfile is a freshly unpacked delegation template, and when it is
// the on-demand copy from a previous run. Writes go in place so the file keeps
// the inode a running container's bind mount points at.
func applyDNSModeCaddyfile(mode string) error {
	dir := filepath.Join(meshployInstDir, "caddy")
	live := filepath.Join(dir, "Caddyfile")
	ondemandPath := filepath.Join(dir, "Caddyfile.ondemand")
	backup := filepath.Join(dir, "Caddyfile.delegation.bak")

	current, err := os.ReadFile(live)
	if err != nil {
		return fmt.Errorf("read %s: %w", live, err)
	}
	ondemand, err := os.ReadFile(ondemandPath)
	if err != nil && mode == "ondemand" {
		return fmt.Errorf("DNS_MODE=ondemand but %s is missing: %w", ondemandPath, err)
	}
	isOndemandCopy := err == nil && bytes.Equal(current, ondemand)

	if mode == "ondemand" {
		if isOndemandCopy {
			return nil
		}
		// current is the delegation template the tarball just unpacked. Keep it
		// as the backup, so switching back restores this release's template and
		// not whichever one the first install happened to have.
		if err := os.WriteFile(backup, current, 0o644); err != nil {
			return fmt.Errorf("save delegation Caddyfile: %w", err)
		}
		fmt.Println("✔  Caddyfile set to the on-demand TLS variant")
		return os.WriteFile(live, ondemand, 0o644)
	}

	// Delegation, or unset, which is what every install before DNS modes was.
	if !isOndemandCopy {
		return nil
	}
	saved, err := os.ReadFile(backup)
	if err != nil {
		// Only reachable with --no-sync; a sync always unpacks the delegation
		// template. The on-demand config works with a delegation too, so keep
		// serving it rather than fail the upgrade.
		fmt.Println("warning: Caddyfile is the on-demand variant and no delegation copy is saved; keeping it")
		return nil
	}
	fmt.Println("✔  Caddyfile restored to the NS-delegation variant")
	return os.WriteFile(live, saved, 0o644)
}

// syncEnvChannel sets MESHPLOY_CHANNEL in /opt/meshploy/.env, updating the
// existing value if present or appending if missing.
func syncEnvChannel(channel string) error {
	return setEnvVar("MESHPLOY_CHANNEL", channel)
}

// setEnvVar rewrites KEY=value in /opt/meshploy/.env, appending it when absent.
//
// .env is the only configuration that survives an upgrade — the deploy tarball
// overwrites everything else in that directory — so it is where a setting that
// must outlive `server-upgrade` belongs.
func setEnvVar(key, value string) error {
	envFile := meshployInstDir + "/.env"
	data, err := os.ReadFile(envFile)
	if err != nil {
		return err
	}
	line := key + "=" + value
	content := string(data)
	if strings.Contains(content, key+"=") {
		lines := strings.Split(content, "\n")
		for i, l := range lines {
			if strings.HasPrefix(l, key+"=") {
				lines[i] = line
			}
		}
		content = strings.Join(lines, "\n")
	} else {
		content = strings.TrimRight(content, "\n") + "\n" + line + "\n"
	}
	return os.WriteFile(envFile, []byte(content), 0600)
}

// readEnvVar returns one value from /opt/meshploy/.env, or "" if unset.
func readEnvVar(key string) string {
	vars, err := parseEnvFile(meshployInstDir+"/.env", key)
	if err != nil {
		return ""
	}
	return vars[key]
}

func detectContainerRuntime() string {
	out, err := exec.Command("bash", "-c",
		`grep '^CONTAINER_RUNTIME=' /opt/meshploy/.env 2>/dev/null | cut -d= -f2 | head -1`).Output()
	if err == nil {
		if rt := strings.TrimSpace(string(out)); rt != "" {
			return rt
		}
	}
	if _, err := exec.LookPath("podman"); err == nil {
		return "podman"
	}
	return "docker"
}

func composeRun(runtime string, args ...string) error {
	return composeExec(meshployInstDir, runtime, args...)
}

func init() {
	serverUpgradeCmd.Flags().String("token", "", "GitHub personal access token for private repo (or set GITHUB_PAT env var)")
	serverUpgradeCmd.Flags().Bool("edge", false, "Sync from main branch and pull edge images instead of latest stable")
	serverUpgradeCmd.Flags().Bool("no-sync", false, "Skip config download — only substitute Corefile, pull images, and restart")
	serverUpgradeCmd.Flags().Bool("no-rollback", false, "On failure, leave the server as it is for inspection instead of putting the previous version back")
	serverUpgradeCmd.Flags().Bool("ee", false, "Switch this install to the Enterprise API image (requires a licence)")
	serverUpgradeCmd.Flags().String("ee-image", "", "Enterprise image to use; defaults to the one this licence grants")
	rootCmd.AddCommand(serverUpgradeCmd)
}

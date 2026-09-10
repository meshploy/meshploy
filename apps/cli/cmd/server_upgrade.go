package cmd

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
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

var serverUpgradeCmd = &cobra.Command{
	Use:   "server-upgrade",
	Short: "Sync deploy configs and pull latest images on this gateway server",
	Long: `Downloads the latest deploy/ configuration from GitHub, substitutes the
Corefile with values from .env, then pulls new container images and restarts.

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

func runServerUpgrade(cmd *cobra.Command, _ []string) error {
	pat, _ := cmd.Flags().GetString("token")
	if pat == "" {
		pat = os.Getenv("GITHUB_PAT")
	}
	edge, _ := cmd.Flags().GetBool("edge")
	noSync, _ := cmd.Flags().GetBool("no-sync")
	ee, _ := cmd.Flags().GetBool("ee")
	eeImage, _ := cmd.Flags().GetString("ee-image")

	if !noSync && os.Getuid() != 0 {
		return fmt.Errorf("must be run as root — try: sudo meshploy server-upgrade")
	}

	runtime := detectContainerRuntime()

	// What Caddy is serving now, to tell afterwards whether it must be recreated.
	// Absent on a broken install; only ever compared, never required.
	caddyfile := filepath.Join(meshployInstDir, "caddy", "Caddyfile")
	caddyBefore, _ := os.ReadFile(caddyfile)

	// Sync MESHPLOY_CHANNEL in .env so image pulls match the chosen channel.
	channel := "latest"
	if edge {
		channel = "main"
	}
	if err := syncEnvChannel(channel); err != nil {
		fmt.Printf("warning: could not update MESHPLOY_CHANNEL in .env: %v\n", err)
	}

	if !noSync {
		ref, err := resolveUpgradeRef(pat, edge)
		if err != nil {
			return err
		}
		if edge {
			fmt.Println("Upgrading from edge (main)…")
		} else {
			fmt.Printf("Upgrading to stable release %s…\n", ref)
		}

		fmt.Println("Syncing deploy configs…")
		if err := downloadDeployTarball(pat, ref); err != nil {
			return err
		}
		fmt.Println("✔  Deploy configs synced")
	}

	// Substitute ${DOMAIN}, ${PUBLIC_IP}, ${MESH_IP} in the Corefile using .env values.
	fmt.Println("Configuring Corefile…")
	if err := substituteCorefile(); err != nil {
		return fmt.Errorf("corefile substitution: %w", err)
	}
	fmt.Println("✔  Corefile configured")

	if err := applyDNSModeCaddyfile(readEnvVar("DNS_MODE")); err != nil {
		return fmt.Errorf("caddyfile: %w", err)
	}
	caddyAfter, _ := os.ReadFile(caddyfile)

	// Enterprise image selection. Explicit --ee switches; otherwise a licensed
	// install running the stock image just gets told, because a routine upgrade
	// should not silently change which product is running.
	scope := entitledRegistryScope()
	if ee {
		if eeImage == "" {
			eeImage = scope
		}
		if err := applyEEImage(runtime, eeImage, pat); err != nil {
			return err
		}
	} else {
		eeNotice(currentAPIImage(), scope)
	}

	fmt.Println("Pulling images…")
	if err := composeRun(runtime, "pull", "--quiet"); err != nil {
		return fmt.Errorf("compose pull: %w", err)
	}

	fmt.Println("Restarting services…")
	if err := composeRun(runtime, "up", "-d", "--remove-orphans"); err != nil {
		return fmt.Errorf("compose up: %w", err)
	}

	// up -d recreates a container only when its compose definition changes, not
	// when a file it mounts does, so a new Caddyfile would sit on disk unserved
	// until Caddy next happened to restart. Certificates live in the caddy_data
	// volume and survive the recreate.
	if !bytes.Equal(caddyBefore, caddyAfter) {
		fmt.Println("Recreating Caddy to load the new Caddyfile…")
		if err := composeRun(runtime, "up", "-d", "--force-recreate", "caddy"); err != nil {
			return fmt.Errorf("recreate caddy: %w", err)
		}
	}

	fmt.Println("✔  Server upgraded successfully")
	return nil
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

func downloadDeployTarball(pat, ref string) error {
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

	tarArgs := []string{"-xz", "--strip-components=2", "-C", meshployInstDir, "--wildcards", "*/deploy"}
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
	composeArgs := append([]string{"compose"}, args...)
	c := exec.Command(runtime, composeArgs...)
	c.Dir = meshployInstDir
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}

func init() {
	serverUpgradeCmd.Flags().String("token", "", "GitHub personal access token for private repo (or set GITHUB_PAT env var)")
	serverUpgradeCmd.Flags().Bool("edge", false, "Sync from main branch and pull edge images instead of latest stable")
	serverUpgradeCmd.Flags().Bool("no-sync", false, "Skip config download — only substitute Corefile, pull images, and restart")
	serverUpgradeCmd.Flags().Bool("ee", false, "Switch this install to the Enterprise API image (requires a licence)")
	serverUpgradeCmd.Flags().String("ee-image", "", "Enterprise image to use; defaults to the one this licence grants")
	rootCmd.AddCommand(serverUpgradeCmd)
}

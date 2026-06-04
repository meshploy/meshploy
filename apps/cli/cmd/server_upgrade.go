package cmd

import (
	"bufio"
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

const (
	meshployRepo    = "meshploy/meshploy"
	meshployInstDir = "/opt/meshploy"
)

var serverUpgradeCmd = &cobra.Command{
	Use:   "server-upgrade",
	Short: "Sync deploy configs and pull latest images on this gateway server",
	Long: `Downloads the latest deploy/ configuration from GitHub, substitutes the
Corefile with values from .env, then pulls new container images and restarts.

Must be run as root on the gateway server (sudo meshploy server-upgrade).

By default pulls the latest stable release. Use --edge to follow the main branch.
Use --no-sync to skip the config download (e.g. when CI has already rsync'd configs).`,
	RunE: runServerUpgrade,
}

func runServerUpgrade(cmd *cobra.Command, _ []string) error {
	pat, _ := cmd.Flags().GetString("token")
	if pat == "" {
		pat = os.Getenv("GITHUB_PAT")
	}
	edge, _ := cmd.Flags().GetBool("edge")
	noSync, _ := cmd.Flags().GetBool("no-sync")

	if !noSync && os.Getuid() != 0 {
		return fmt.Errorf("must be run as root — try: sudo meshploy server-upgrade")
	}

	runtime := detectContainerRuntime()

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

	fmt.Println("Pulling images…")
	if err := composeRun(runtime, "pull", "--quiet"); err != nil {
		return fmt.Errorf("compose pull: %w", err)
	}

	fmt.Println("Restarting services…")
	if err := composeRun(runtime, "up", "-d", "--remove-orphans"); err != nil {
		return fmt.Errorf("compose up: %w", err)
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
	rootCmd.AddCommand(serverUpgradeCmd)
}

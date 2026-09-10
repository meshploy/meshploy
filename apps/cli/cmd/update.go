package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const githubRepo = "meshploy/meshploy"

// githubAPI is where releases are read from. A var so tests can serve releases
// from a local server.
var githubAPI = "https://api.github.com"

// checksumAsset is what the release workflow publishes beside the binaries.
const checksumAsset = "SHA256SUMS"

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Download and replace the CLI binary with the latest stable release",
	RunE: func(cmd *cobra.Command, args []string) error {
		pat, _ := cmd.Flags().GetString("token")
		if pat == "" {
			pat = os.Getenv("GITHUB_PAT")
		}
		edge, _ := cmd.Flags().GetBool("edge")

		exePath, err := os.Executable()
		if err != nil {
			return fmt.Errorf("resolve binary path: %w", err)
		}
		assetName := fmt.Sprintf("meshploy-linux-%s", runtime.GOARCH) // amd64 or arm64
		return updateCLI(cmd.OutOrStdout(), pat, edge, exePath, assetName)
	},
}

// updateCLI replaces the binary at exePath with assetName from the newest
// release on the channel, checked against the release's SHA256SUMS first.
func updateCLI(w io.Writer, pat string, edge bool, exePath, assetName string) error {
	var channel string
	if edge {
		channel = "cli-latest"
		fmt.Fprintln(w, "Fetching edge release…")
	} else {
		channel = "latest"
		fmt.Fprintln(w, "Fetching latest stable release…")
	}

	assets, err := releaseAssets(pat, channel)
	if err != nil {
		return err
	}
	assetURL := assets[assetName]
	if assetURL == "" {
		return fmt.Errorf("no asset %q found in release %s", assetName, channel)
	}

	// The checksums come from the same release as the binary, so they catch a
	// corrupted or truncated download, not a compromised release. A release
	// published before they existed has none; refusing it would strand every
	// install on that release, so it is installed with a warning.
	want := ""
	if sumsURL := assets[checksumAsset]; sumsURL != "" {
		if want, err = expectedChecksum(pat, sumsURL, assetName); err != nil {
			return err
		}
	} else {
		fmt.Fprintf(w, "warning: this release publishes no %s, so the download cannot be verified\n", checksumAsset)
	}

	fmt.Fprintf(w, "Downloading %s…\n", assetName)
	if err := downloadReplace(pat, assetURL, exePath, want); err != nil {
		return err
	}
	if want != "" {
		fmt.Fprintf(w, "✔  Verified against the release's %s\n", checksumAsset)
	}
	fmt.Fprintf(w, "✔  meshploy updated at %s\n", exePath)
	return nil
}

// releaseAssets returns the URL of every asset in the release, keyed by name.
// These are API URLs rather than browser_download_url: for a private repo the
// browser URL redirects through storage that drops the Authorization header.
func releaseAssets(pat, channel string) (map[string]string, error) {
	var url string
	if channel == "latest" {
		url = fmt.Sprintf("%s/repos/%s/releases/latest", githubAPI, githubRepo)
	} else {
		url = fmt.Sprintf("%s/repos/%s/releases/tags/%s", githubAPI, githubRepo, channel)
	}
	req, _ := http.NewRequest("GET", url, nil)
	if pat != "" {
		req.Header.Set("Authorization", "token "+pat)
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch release: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GitHub API %d: %s", resp.StatusCode, string(b))
	}

	var release struct {
		Assets []struct {
			Name string `json:"name"`
			URL  string `json:"url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("parse release: %w", err)
	}
	assets := make(map[string]string, len(release.Assets))
	for _, a := range release.Assets {
		assets[a.Name] = a.URL
	}
	return assets, nil
}

// expectedChecksum reads the release's SHA256SUMS and returns the hash it
// lists for name.
func expectedChecksum(pat, sumsURL, name string) (string, error) {
	resp, err := getAsset(pat, sumsURL, 30*time.Second)
	if err != nil {
		return "", fmt.Errorf("download %s: %w", checksumAsset, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return "", fmt.Errorf("read %s: %w", checksumAsset, err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		// sha256sum writes "<hash>  <name>", or "<hash> *<name>" in binary mode.
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == name {
			return strings.ToLower(f[0]), nil
		}
	}
	return "", fmt.Errorf("%s lists no checksum for %s", checksumAsset, name)
}

// getAsset starts the download of one release asset.
func getAsset(pat, url string, timeout time.Duration) (*http.Response, error) {
	req, _ := http.NewRequest("GET", url, nil)
	if pat != "" {
		req.Header.Set("Authorization", "token "+pat)
	}
	// GitHub requires Accept: application/octet-stream to get the raw file.
	req.Header.Set("Accept", "application/octet-stream")

	resp, err := (&http.Client{Timeout: timeout}).Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(b))
	}
	return resp, nil
}

// downloadReplace downloads the binary to a temp file in the same directory,
// checks it against wantSHA when one is given, then atomically renames it over
// the current binary. A download that does not match never reaches dest.
func downloadReplace(pat, assetURL, dest, wantSHA string) error {
	resp, err := getAsset(pat, assetURL, 2*time.Minute)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()

	// Write to a temp file in the same directory as the binary so rename is
	// atomic (cross-device rename fails when /tmp is a separate filesystem).
	tmp, err := os.CreateTemp(filepath.Dir(dest), "meshploy-update-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // cleaned up if rename succeeds this is a no-op

	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, h), resp.Body); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	tmp.Close()

	if got := hex.EncodeToString(h.Sum(nil)); wantSHA != "" && got != wantSHA {
		return fmt.Errorf("the download does not match the release's %s (got %s, want %s); the installed CLI was not replaced",
			checksumAsset, got, wantSHA)
	}

	if err := os.Chmod(tmpPath, 0755); err != nil {
		return fmt.Errorf("chmod: %w", err)
	}

	if err := os.Rename(tmpPath, dest); err != nil {
		return fmt.Errorf("replace binary (may need sudo): %w", err)
	}
	return nil
}

func init() {
	updateCmd.Flags().String("token", "", "GitHub personal access token (or set GITHUB_PAT env var)")
	updateCmd.Flags().Bool("edge", false, "Install the edge build from main instead of the latest stable release")
	rootCmd.AddCommand(updateCmd)
}

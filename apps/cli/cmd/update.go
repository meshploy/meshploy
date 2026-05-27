package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/spf13/cobra"
)

const githubRepo = "meshploy/meshploy"

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Download and replace the CLI binary with the latest stable release",
	RunE: func(cmd *cobra.Command, args []string) error {
		pat, _ := cmd.Flags().GetString("token")
		if pat == "" {
			pat = os.Getenv("GITHUB_PAT")
		}
		edge, _ := cmd.Flags().GetBool("edge")

		arch := runtime.GOARCH // amd64 or arm64
		assetName := fmt.Sprintf("meshploy-linux-%s", arch)

		exePath, err := os.Executable()
		if err != nil {
			return fmt.Errorf("resolve binary path: %w", err)
		}

		var channel string
		if edge {
			channel = "cli-latest"
			fmt.Println("Fetching edge release…")
		} else {
			channel = "latest"
			fmt.Println("Fetching latest stable release…")
		}

		assetURL, err := resolveAssetURL(pat, assetName, channel)
		if err != nil {
			return err
		}

		fmt.Printf("Downloading %s…\n", assetName)
		if err := downloadReplace(pat, assetURL, exePath); err != nil {
			return err
		}

		fmt.Printf("✔  meshploy updated at %s\n", exePath)
		return nil
	},
}

func resolveAssetURL(pat, assetName, channel string) (string, error) {
	var url string
	if channel == "latest" {
		url = fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", githubRepo)
	} else {
		url = fmt.Sprintf("https://api.github.com/repos/%s/releases/tags/%s", githubRepo, channel)
	}
	req, _ := http.NewRequest("GET", url, nil)
	if pat != "" {
		req.Header.Set("Authorization", "token "+pat)
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch release: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("GitHub API %d: %s", resp.StatusCode, string(b))
	}

	var release struct {
		Assets []struct {
			Name string `json:"name"`
			URL  string `json:"url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", fmt.Errorf("parse release: %w", err)
	}

	for _, a := range release.Assets {
		if a.Name == assetName {
			return a.URL, nil
		}
	}
	return "", fmt.Errorf("no asset %q found in release %s", assetName, channel)
}

// downloadReplace downloads the binary to a temp file in the same directory,
// then atomically renames it over the current binary.
func downloadReplace(pat, assetURL, dest string) error {
	req, _ := http.NewRequest("GET", assetURL, nil)
	if pat != "" {
		req.Header.Set("Authorization", "token "+pat)
	}
	// GitHub requires Accept: application/octet-stream to get the raw binary.
	req.Header.Set("Accept", "application/octet-stream")

	httpClient := &http.Client{Timeout: 2 * time.Minute}
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("download HTTP %d: %s", resp.StatusCode, string(b))
	}

	// Write to a temp file in the same directory as the binary so rename is
	// atomic (cross-device rename fails when /tmp is a separate filesystem).
	tmp, err := os.CreateTemp(filepath.Dir(dest), "meshploy-update-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // cleaned up if rename succeeds this is a no-op

	if _, err := io.Copy(tmp, resp.Body); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	tmp.Close()

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

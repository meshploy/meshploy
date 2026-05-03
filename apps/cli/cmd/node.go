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
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/meshploy/apps/cli/internal/client"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

const (
	installScript   = "/opt/meshploy/install.sh"
	uninstallScript = "/opt/meshploy/uninstall.sh"
	deployEnvFile   = "/opt/meshploy/.env"
)

var nodeCmd = &cobra.Command{
	Use:   "node",
	Short: "Manage mesh nodes",
}

// ── node list ─────────────────────────────────────────────────────────────────

var nodeListCmd = &cobra.Command{
	Use:   "list",
	Short: "List nodes in the cluster",
	RunE: func(cmd *cobra.Command, args []string) error {
		c := apiClient()
		nodes, err := c.ListNodes(orgID())
		if err != nil {
			return err
		}
		if len(nodes) == 0 {
			fmt.Println("No nodes found.")
			return nil
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tNAME\tSTATUS\tROLE\tIP")
		for _, n := range nodes {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", n.ID, n.Name, n.Status, n.K3sRole, n.TailscaleIP)
		}
		return w.Flush()
	},
}

// ── node delete ───────────────────────────────────────────────────────────────

var nodeDeleteCmd = &cobra.Command{
	Use:   "delete <node-id>",
	Short: "Delete a node by ID",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		nodeID := args[0]
		yes, _ := cmd.Flags().GetBool("yes")
		if !yes {
			fmt.Printf("Delete node %s? This removes it from Headscale, k3s, and the Meshploy DB. [y/N]: ", nodeID)
			var answer string
			fmt.Scanln(&answer)
			if answer != "y" && answer != "Y" {
				fmt.Println("Aborted.")
				return nil
			}
		}
		c := apiClient()
		if err := c.DeleteNode(orgID(), nodeID); err != nil {
			return err
		}
		fmt.Printf("✔  Node %s deleted.\n", nodeID)
		return nil
	},
}

var nodeRemoveCmd = &cobra.Command{
	Use:   "remove <name> <user@host>",
	Short: "Uninstall a node cleanly and remove it from the cluster",
	Long: `SSHes into the node, stops and removes k3s agent and Tailscale,
then deletes the node record from Headscale and the Meshploy DB.

Works with both root and non-root users — sudo password will be prompted
if required (a PTY is allocated for the SSH session).

Examples:
  meshploy node remove enthesus-pc feds@192.168.0.140
  meshploy node remove worker-1 root@10.0.0.5 --identity-file ~/.ssh/id_ed25519`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		name        := args[0]
		sshTarget   := args[1]
		yes, _      := cmd.Flags().GetBool("yes")
		identityFile, _ := cmd.Flags().GetString("identity-file")
		sshPort, _  := cmd.Flags().GetInt("port")

		// ── Look up node by name ──────────────────────────────────────────────
		c := apiClient()
		nodes, err := c.ListNodes(orgID())
		if err != nil {
			return fmt.Errorf("list nodes: %w", err)
		}
		var found *client.Node
		for i := range nodes {
			if nodes[i].Name == name {
				found = &nodes[i]
				break
			}
		}
		if found == nil {
			return fmt.Errorf("no node named %q", name)
		}

		if !yes {
			fmt.Printf("Remove node %q (%s) via %s?\n", found.Name, found.ID, sshTarget)
			fmt.Print("This will uninstall k3s agent, disconnect Tailscale, and delete the node record. [y/N]: ")
			var answer string
			fmt.Scanln(&answer)
			if answer != "y" && answer != "Y" {
				fmt.Println("Aborted.")
				return nil
			}
		}

		// ── Download uninstall script from API ────────────────────────────────
		fmt.Println("\nDownloading uninstall script…")
		uninstallURL := strings.TrimRight(loadedCfg.APIURL, "/") + "/uninstall.sh"
		req, _ := http.NewRequest("GET", uninstallURL, nil)
		req.Header.Set("Authorization", "Bearer "+loadedCfg.Token)
		httpClient := &http.Client{Timeout: 30 * time.Second}
		resp, err := httpClient.Do(req)
		if err != nil || resp.StatusCode != 200 {
			if resp != nil {
				resp.Body.Close()
			}
			return fmt.Errorf("download uninstall script: could not fetch from %s", uninstallURL)
		}
		scriptBytes, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return fmt.Errorf("read uninstall script: %w", err)
		}
		scriptContent := string(scriptBytes)

		// ── Base SSH args ─────────────────────────────────────────────────────
		baseArgs := []string{"-o", "StrictHostKeyChecking=accept-new"}
		if identityFile != "" {
			baseArgs = append(baseArgs, "-i", identityFile)
		}
		if sshPort != 22 {
			baseArgs = append(baseArgs, "-p", fmt.Sprintf("%d", sshPort))
		}

		fmt.Printf("\nConnecting to %s…\n\n", sshTarget)

		// Step 1: upload uninstall script (no PTY needed)
		tmpScript := "/tmp/.meshploy-uninstall.sh"
		step1 := exec.Command("ssh", append(baseArgs, sshTarget,
			fmt.Sprintf("cat > %s && chmod +x %s", tmpScript, tmpScript))...)
		step1.Stdin = strings.NewReader(scriptContent)
		step1.Stderr = os.Stderr
		if err := step1.Run(); err != nil {
			return fmt.Errorf("upload uninstall script: %w", err)
		}

		// Step 2: execute with PTY — sudo can prompt for password, script handles
		// k3s removal, self-deregistration via /etc/meshploy/node.conf, and Tailscale logout.
		step2 := exec.Command("ssh", append(baseArgs, "-t", sshTarget,
			fmt.Sprintf("TERM=xterm-256color bash %s --worker --yes; _rc=$?; rm -f %s; exit $_rc", tmpScript, tmpScript))...)
		step2.Stdin = os.Stdin
		step2.Stdout = os.Stdout
		step2.Stderr = os.Stderr
		if err := step2.Run(); err != nil {
			return fmt.Errorf("remote uninstall failed: %w", err)
		}

		// ── Delete from DB + Headscale (fallback if self-deregister failed) ───
		fmt.Printf("\nRemoving node record from Meshploy…\n")
		if err := c.DeleteNode(orgID(), found.ID); err != nil {
			// Non-fatal: self-deregister in uninstall.sh may have already removed it.
			fmt.Printf("  (node record already removed or not found: %v)\n", err)
		}
		fmt.Printf("✔  Node %q removed.\n", found.Name)
		return nil
	},
}

// ── node status ───────────────────────────────────────────────────────────────

var nodeStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show this node's local identity (/etc/meshploy/node.conf)",
	RunE: func(cmd *cobra.Command, args []string) error {
		const confFile = "/etc/meshploy/node.conf"
		data, err := os.ReadFile(confFile)
		if err != nil {
			if os.IsNotExist(err) {
				fmt.Println("Not registered as a worker node (/etc/meshploy/node.conf not found).")
				return nil
			}
			return err
		}
		fmt.Printf("Node identity (%s):\n%s\n", confFile, string(data))
		return nil
	},
}

// ── node install ──────────────────────────────────────────────────────────────

var nodeInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Install and join this machine as a Meshploy node",
	Long: `Runs /opt/meshploy/install.sh interactively.

Requires root. The install script handles dependency installation,
k3s setup, Headscale registration, and saves /etc/meshploy/node.conf.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if os.Getuid() != 0 {
			return fmt.Errorf("node install requires root — re-run with sudo")
		}
		return runScript(installScript, nil)
	},
}

// ── node uninstall ────────────────────────────────────────────────────────────

var nodeUninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Uninstall this node and deregister from the cluster",
	Long: `Runs /opt/meshploy/uninstall.sh interactively.

Deregisters the node from Meshploy (via API), removes k3s agent,
and disconnects from the Headscale mesh.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if os.Getuid() != 0 {
			return fmt.Errorf("node uninstall requires root — re-run with sudo")
		}
		var extraArgs []string
		if yes, _ := cmd.Flags().GetBool("yes"); yes {
			extraArgs = append(extraArgs, "--yes")
		}
		return runScript(uninstallScript, extraArgs)
	},
}

// ── node token ────────────────────────────────────────────────────────────────

var nodeTokenCmd = &cobra.Command{
	Use:   "token",
	Short: "Show or rotate the node registration token",
}

var nodeTokenGetCmd = &cobra.Command{
	Use:   "get",
	Short: "Show the current registration token",
	RunE: func(cmd *cobra.Command, args []string) error {
		c := apiClient()
		token, err := c.GetRegistrationToken(orgID())
		if err != nil {
			return err
		}
		if token == "" {
			fmt.Println("No registration token found. Run: meshploy node token rotate")
			return nil
		}
		fmt.Println(token)
		return nil
	},
}

var nodeTokenRotateCmd = &cobra.Command{
	Use:   "rotate",
	Short: "Generate a new registration token (invalidates the old one)",
	RunE: func(cmd *cobra.Command, args []string) error {
		c := apiClient()
		token, err := c.RotateRegistrationToken(orgID())
		if err != nil {
			return err
		}
		fmt.Println(token)
		return nil
	},
}

// ── node init ─────────────────────────────────────────────────────────────────

var nodeInitCmd = &cobra.Command{
	Use:   "init <user@host>",
	Short: "Initialize a remote machine as a Meshploy node over SSH",
	Long: `Connects to a remote machine via SSH, pipes the node installer, and
automatically passes all required values (API URL, registration token,
Headscale URL, preauth key, k3s join token) so the machine joins the
mesh without any manual steps.

Values are read from /opt/meshploy/.env on the master. A fresh Headscale
preauth key is generated automatically; use --preauth-key to override.

Examples:
  meshploy node init root@192.168.1.10
  meshploy node init ubuntu@10.0.0.5 --identity-file ~/.ssh/id_ed25519
  meshploy node init admin@worker.internal --port 2222`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		identityFile, _ := cmd.Flags().GetString("identity-file")
		sshPort, _ := cmd.Flags().GetInt("port")
		preauthKeyOverride, _ := cmd.Flags().GetString("preauth-key")

		if _, err := os.Stat(installScript); os.IsNotExist(err) {
			return fmt.Errorf(
				"install script not found at %s\n\n"+
					"meshploy node init must be run from the master node where Meshploy is installed.",
				installScript,
			)
		}

		// Fetch (or create) a registration token.
		c := apiClient()
		regToken, err := c.GetRegistrationToken(orgID())
		if err != nil {
			return fmt.Errorf("fetch registration token: %w", err)
		}
		if regToken == "" {
			regToken, err = c.RotateRegistrationToken(orgID())
			if err != nil {
				return fmt.Errorf("create registration token: %w", err)
			}
		}

		// Always-present env vars.
		envVars := map[string]string{
			"MESHPLOY_API_URL": loadedCfg.APIURL,
			"MESHPLOY_TOKEN":   regToken,
			"NODE_TYPE":        "worker",
		}

		// Enrich from /opt/meshploy/.env when available.
		dotenv, err := parseDotEnv(deployEnvFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not read %s: %v\n", deployEnvFile, err)
			fmt.Fprintln(os.Stderr, "         HEADSCALE_URL, K3S_SERVER_URL and K3S_JOIN_TOKEN will not be pre-filled.")
		} else {
			domain := dotenv["DOMAIN"]
			meshIP := dotenv["MESH_IP"]
			k3sToken := dotenv["K3S_TOKEN"]
			headscaleAPIKey := dotenv["HEADSCALE_API_KEY"]

			if domain != "" {
				envVars["HEADSCALE_URL"] = "https://headscale." + domain
			}
			if meshIP != "" {
				envVars["K3S_SERVER_URL"] = "https://" + meshIP + ":6443"
			}
			if k3sToken != "" {
				envVars["K3S_JOIN_TOKEN"] = k3sToken
			}

			// Generate a fresh Headscale preauth key (or use the override).
			if preauthKeyOverride != "" {
				envVars["PREAUTH_KEY"] = preauthKeyOverride
			} else if domain != "" && headscaleAPIKey != "" {
				headscaleURL := "https://headscale." + domain
				fmt.Println("Generating Headscale preauth key…")
				key, keyErr := headscaleCreatePreauthKey(headscaleURL, headscaleAPIKey)
				if keyErr != nil {
					fmt.Fprintf(os.Stderr, "warning: could not generate Headscale preauth key: %v\n", keyErr)
					fmt.Fprintln(os.Stderr, "         The installer will prompt for it interactively.")
				} else {
					envVars["PREAUTH_KEY"] = key
					fmt.Println("✔  Headscale preauth key generated.")
				}
			} else {
				fmt.Fprintln(os.Stderr, "warning: HEADSCALE_API_KEY not found in .env — preauth key will be prompted interactively.")
			}
		}

		// Build SSH argument list.
		sshArgs := []string{"-o", "StrictHostKeyChecking=accept-new"}
		if identityFile != "" {
			sshArgs = append(sshArgs, "-i", identityFile)
		}
		if sshPort != 22 {
			sshArgs = append(sshArgs, "-p", fmt.Sprintf("%d", sshPort))
		}
		sshArgs = append(sshArgs, args[0])

		// Build the env prefix for the remote command.
		envParts := make([]string, 0, len(envVars))
		for k, v := range envVars {
			envParts = append(envParts, k+"="+shellQuote(v))
		}
		// bash -s -- --auto: positional args after "--" are passed to the script as $@.
		remoteCmd := fmt.Sprintf("env %s bash -s -- --auto", strings.Join(envParts, " "))
		sshArgs = append(sshArgs, remoteCmd)

		fmt.Printf("Connecting to %s and running node installer…\n\n", args[0])

		f, err := os.Open(installScript)
		if err != nil {
			return err
		}
		defer f.Close()

		ssh := exec.Command("ssh", sshArgs...)
		ssh.Stdin = f
		ssh.Stdout = os.Stdout
		ssh.Stderr = os.Stderr
		return ssh.Run()
	},
}

// shellQuote wraps a string in single quotes, escaping any embedded single quotes.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

// parseDotEnv parses a KEY=VALUE file, ignoring blank lines and comments.
func parseDotEnv(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string)
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		// Strip surrounding quotes if present.
		if len(v) >= 2 && ((v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'')) {
			v = v[1 : len(v)-1]
		}
		out[k] = v
	}
	return out, nil
}

// headscaleCreatePreauthKey generates a fresh 1-hour reusable preauth key via
// the Headscale REST API, scoped to the "meshploy" user.
func headscaleCreatePreauthKey(headscaleURL, apiKey string) (string, error) {
	hc := &http.Client{Timeout: 10 * time.Second}

	// Resolve the numeric user ID for "meshploy".
	req, err := http.NewRequest(http.MethodGet, headscaleURL+"/api/v1/user", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := hc.Do(req)
	if err != nil {
		return "", fmt.Errorf("list users: %w", err)
	}
	defer resp.Body.Close()

	var userBody struct {
		Users []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"users"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&userBody); err != nil {
		return "", fmt.Errorf("list users decode: %w", err)
	}
	var userID string
	for _, u := range userBody.Users {
		if u.Name == "meshploy" {
			userID = u.ID
			break
		}
	}
	if userID == "" {
		return "", fmt.Errorf("headscale user 'meshploy' not found")
	}

	// Create a 1-hour reusable preauth key.
	payload, _ := json.Marshal(map[string]any{
		"user":       userID,
		"reusable":   true,
		"ephemeral":  false,
		"expiration": time.Now().Add(time.Hour).Format(time.RFC3339),
	})
	req2, err := http.NewRequest(http.MethodPost, headscaleURL+"/api/v1/preauthkey", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req2.Header.Set("Authorization", "Bearer "+apiKey)
	req2.Header.Set("Content-Type", "application/json")
	resp2, err := hc.Do(req2)
	if err != nil {
		return "", fmt.Errorf("create preauth key: %w", err)
	}
	defer resp2.Body.Close()

	var keyBody struct {
		PreAuthKey struct {
			Key string `json:"key"`
		} `json:"preAuthKey"`
	}
	if err := json.NewDecoder(resp2.Body).Decode(&keyBody); err != nil {
		return "", fmt.Errorf("decode preauth key: %w", err)
	}
	if keyBody.PreAuthKey.Key == "" {
		return "", fmt.Errorf("headscale returned empty preauth key (HTTP %d)", resp2.StatusCode)
	}
	return keyBody.PreAuthKey.Key, nil
}

// ── node add ──────────────────────────────────────────────────────────────────

var nodeAddCmd = &cobra.Command{
	Use:   "add <user@host>",
	Short: "Bootstrap a remote machine as a worker node (runs from any machine)",
	Long: `Downloads the Meshploy install script and runs it on the target machine
over SSH. Unlike 'node init', this command can run from any machine — it
does not require the master node's local files.

It auto-fetches the registration token from the API. You will be prompted
for the Headscale URL and a preauth key (generate one in the Meshploy UI
under Cluster → Add a worker node, or via 'node init' on the master).

Examples:
  meshploy node add root@192.168.1.10
  meshploy node add ubuntu@10.0.0.5 --identity-file ~/.ssh/id_ed25519
  meshploy node add admin@worker.internal --headscale-url https://headscale.example.com`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		identityFile, _ := cmd.Flags().GetString("identity-file")
		sshPort, _      := cmd.Flags().GetInt("port")
		headscaleURL, _ := cmd.Flags().GetString("headscale-url")
		preauthKey, _   := cmd.Flags().GetString("preauth-key")
		githubPAT, _    := cmd.Flags().GetString("token")

		sc := bufio.NewScanner(os.Stdin)

		// ── Registration token (auto) ─────────────────────────────────────────
		c := apiClient()
		regToken, err := c.GetRegistrationToken(orgID())
		if err != nil {
			return fmt.Errorf("fetch registration token: %w", err)
		}
		if regToken == "" {
			fmt.Println("No registration token found, generating one…")
			regToken, err = c.RotateRegistrationToken(orgID())
			if err != nil {
				return fmt.Errorf("create registration token: %w", err)
			}
		}

		// ── Headscale URL (guess or prompt) ───────────────────────────────────
		if headscaleURL == "" {
			// Heuristic: api.example.com → headscale.example.com
			guessed := strings.Replace(loadedCfg.APIURL, "://api.", "://headscale.", 1)
			if guessed == loadedCfg.APIURL {
				guessed = ""
			}
			headscaleURL = promptDefault(sc, "Headscale URL", guessed)
		}
		if headscaleURL == "" {
			return fmt.Errorf("headscale URL is required (use --headscale-url or enter it at the prompt)")
		}

		// ── Preauth key (prompt if not provided) ──────────────────────────────
		if preauthKey == "" {
			fmt.Printf("\nGenerate a preauth key at: %s → Cluster → Add a worker node\n\n", strings.Replace(loadedCfg.APIURL, "://api.", "://app.", 1))
			fmt.Print("Preauth key: ")
			keyBytes, err := term.ReadPassword(int(syscall.Stdin))
			fmt.Println()
			if err != nil {
				return fmt.Errorf("read preauth key: %w", err)
			}
			preauthKey = strings.TrimSpace(string(keyBytes))
		}
		if preauthKey == "" {
			return fmt.Errorf("preauth key is required")
		}

		// ── Node scheduling role ──────────────────────────────────────────────
		fmt.Println()
		fmt.Println("Node scheduling role:")
		fmt.Println("  1) workload_builder (default) — runs workloads AND build jobs")
		fmt.Println("  2) workload                   — workloads only")
		fmt.Println("  3) builder                    — build jobs only")
		fmt.Println()
		roleChoice := promptDefault(sc, "Role [1/2/3]", "1")
		var nodeRoleChoice string
		switch roleChoice {
		case "2":
			nodeRoleChoice = "2"
		case "3":
			nodeRoleChoice = "3"
		default:
			nodeRoleChoice = "1"
		}

		// ── Download install script ───────────────────────────────────────────
		fmt.Println("\nDownloading install script…")
		scriptContent, err := downloadInstallScript(loadedCfg.APIURL, loadedCfg.Token, githubPAT)
		if err != nil {
			return fmt.Errorf("download install script: %w", err)
		}

		// ── Build env vars ────────────────────────────────────────────────────
		meshAPIURL := "http://100.64.0.1:4000"
		envVars := map[string]string{
			"NODE_TYPE":        "worker",
			"HEADSCALE_URL":    headscaleURL,
			"PREAUTH_KEY":      preauthKey,
			"MESHPLOY_TOKEN":   regToken,
			"NODE_ROLE_CHOICE": nodeRoleChoice,
			"MESHPLOY_API_URL": meshAPIURL,
			"TERM":             "xterm-256color",
		}

		// ── Base SSH args (no PTY) ────────────────────────────────────────────
		baseArgs := []string{"-o", "StrictHostKeyChecking=accept-new"}
		if identityFile != "" {
			baseArgs = append(baseArgs, "-i", identityFile)
		}
		if sshPort != 22 {
			baseArgs = append(baseArgs, "-p", fmt.Sprintf("%d", sshPort))
		}

		envParts := make([]string, 0, len(envVars))
		for k, v := range envVars {
			envParts = append(envParts, k+"="+shellQuote(v))
		}
		envPrefix := strings.Join(envParts, " ")

		fmt.Printf("Connecting to %s…\n\n", args[0])

		// ── Step 1: pipe script to temp file (no PTY needed) ─────────────────
		tmpScript := "/tmp/.meshploy-install.sh"
		step1 := exec.Command("ssh", append(baseArgs, args[0], fmt.Sprintf("cat > %s && chmod +x %s", tmpScript, tmpScript))...)
		step1.Stdin = strings.NewReader(scriptContent)
		step1.Stderr = os.Stderr
		if err := step1.Run(); err != nil {
			return fmt.Errorf("upload install script: %w", err)
		}

		// ── Step 2: execute with PTY so sudo can prompt for password ─────────
		step2 := exec.Command("ssh", append(baseArgs, "-t", args[0],
			fmt.Sprintf("env %s bash %s --auto; _rc=$?; rm -f %s; exit $_rc", envPrefix, tmpScript, tmpScript))...)
		step2.Stdin = os.Stdin
		step2.Stdout = os.Stdout
		step2.Stderr = os.Stderr
		return step2.Run()
	},
}

// downloadInstallScript fetches install.sh from the Meshploy API (primary,
// authenticated) or from GitHub raw (fallback, requires --token for private repos).
func downloadInstallScript(apiURL, apiToken, githubPAT string) (string, error) {
	type attempt struct {
		url   string
		token string
		auth  string // header prefix: "Bearer" or "token"
	}
	attempts := []attempt{
		{apiURL + "/install.sh", apiToken, "Bearer"},
		{"https://raw.githubusercontent.com/" + githubRepo + "/main/deploy/install.sh", githubPAT, "token"},
	}

	for _, a := range attempts {
		if a.token == "" {
			continue
		}
		req, _ := http.NewRequest("GET", a.url, nil)
		req.Header.Set("Authorization", a.auth+" "+a.token)
		client := &http.Client{Timeout: 30 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			continue
		}
		b, err := io.ReadAll(resp.Body)
		return string(b), err
	}
	return "", fmt.Errorf("could not fetch install script — ensure you are logged in or pass --token for private GitHub repos")
}

func init() {
	nodeDeleteCmd.Flags().BoolP("yes", "y", false, "Skip confirmation prompt")
	nodeUninstallCmd.Flags().BoolP("yes", "y", false, "Skip confirmation prompts")

	nodeInitCmd.Flags().StringP("identity-file", "i", "", "SSH identity file (private key)")
	nodeInitCmd.Flags().IntP("port", "P", 22, "SSH port on the remote host")
	nodeInitCmd.Flags().StringP("preauth-key", "k", "", "Headscale preauth key (auto-generated from /opt/meshploy/.env if omitted)")

	nodeAddCmd.Flags().StringP("identity-file", "i", "", "SSH identity file (private key)")
	nodeAddCmd.Flags().IntP("port", "P", 22, "SSH port on the remote host")
	nodeAddCmd.Flags().String("headscale-url", "", "Headscale URL (e.g. https://headscale.example.com)")
	nodeAddCmd.Flags().StringP("preauth-key", "k", "", "Headscale preauth key")
	nodeAddCmd.Flags().String("token", "", "GitHub PAT for downloading install script (or set GITHUB_PAT env var)")

	nodeTokenCmd.AddCommand(nodeTokenGetCmd, nodeTokenRotateCmd)
	nodeRemoveCmd.Flags().BoolP("yes", "y", false, "Skip confirmation prompt")
	nodeRemoveCmd.Flags().StringP("identity-file", "i", "", "SSH identity file (private key)")
	nodeRemoveCmd.Flags().IntP("port", "P", 22, "SSH port on the remote host")
	nodeRemoveCmd.Flags().String("token", "", "GitHub PAT for fallback script download (or set GITHUB_PAT)")
	nodeCmd.AddCommand(nodeListCmd, nodeDeleteCmd, nodeRemoveCmd, nodeStatusCmd, nodeInstallCmd, nodeUninstallCmd, nodeTokenCmd, nodeInitCmd, nodeAddCmd)
	rootCmd.AddCommand(nodeCmd)
}

// runScript execs a bash script, inheriting stdin/stdout/stderr so interactive
// prompts work correctly.
func runScript(scriptPath string, args []string) error {
	if _, err := os.Stat(scriptPath); os.IsNotExist(err) {
		return fmt.Errorf("script not found: %s\n\nMeshploy must be installed at /opt/meshploy/", scriptPath)
	}
	cmdArgs := append([]string{scriptPath}, args...)
	c := exec.Command("bash", cmdArgs...)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}

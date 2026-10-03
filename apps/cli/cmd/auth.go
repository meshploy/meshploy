package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/meshploy/apps/cli/internal/config"
	"github.com/meshploy/packages/client"
	"github.com/spf13/cobra"
)

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Manage authentication",
}

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Log in to a Meshploy server, approving in a browser",
	Long: `Log in to a Meshploy server.

Prints a link and a code. Open the link in any browser (it need not be on this
machine), sign in there, check the code matches, and approve: this CLI is then
logged in as you, until you log it out or it goes 90 days unused. Your password
and two-factor code are never typed here.

The server is found from --url, then this machine (the gateway's
/opt/meshploy/.env, or a worker's /etc/meshploy/node.conf), then the last login,
and otherwise asked for. --url takes a domain (example.com) or a console, Apps
or API address.

--password logs in the old way, typing email, password and two-factor code
here, for a machine that cannot reach a browser at all.`,
	RunE: runLogin,
}

// loginServer is the server a login goes to: the API, the console name to
// approve at, and where it was found, to say so.
type loginServer struct {
	api     string
	door    string
	from    string // empty when the person named it, by --url or the prompt
	keepAPI bool   // keep api as reached, rather than the address the server gives
}

func runLogin(cmd *cobra.Command, _ []string) error {
	sc := bufio.NewScanner(os.Stdin)
	srv, err := findLoginServer(cmd, sc)
	if err != nil {
		return err
	}
	if usePassword, _ := cmd.Flags().GetBool("password"); usePassword {
		return passwordLogin(cmd, sc, srv.api)
	}

	host, _ := os.Hostname()
	login, err := client.New(srv.api, "").StartCLILogin(host, srv.door)
	if err != nil {
		return fmt.Errorf("start login: %w", err)
	}
	api := srv.api
	if login.APIURL != "" && !srv.keepAPI {
		api = strings.TrimRight(login.APIURL, "/")
	}

	fmt.Printf("\nOpen this link in a browser, sign in, and check it shows the code %s:\n\n  %s\n\n", login.UserCode, login.VerificationURL)
	if noBrowser, _ := cmd.Flags().GetBool("no-browser"); !noBrowser && openBrowser(login.VerificationURL) {
		fmt.Println("(Opened in your browser.)")
	}
	fmt.Println("Waiting for approval… (Ctrl-C to cancel)")

	token, err := waitForApproval(srv.api, login)
	if err != nil {
		return err
	}
	return saveLogin(api, token)
}

// findLoginServer settles the server in the terminal, before any browser:
// over SSH the browser is on another machine and can hand nothing back.
func findLoginServer(cmd *cobra.Command, sc *bufio.Scanner) (loginServer, error) {
	typed, _ := cmd.Flags().GetString("url")
	if typed == "" {
		typed = cfgAPIURL // the older --api-url, still accepted
	}
	if typed != "" {
		return reachServer(typed, "")
	}

	// Found on this machine or remembered: tried in turn, and one that does
	// not answer is passed over rather than stopping the login.
	var found []loginServer
	if env, err := parseDotEnv(deployEnvFile); err == nil && env["API_BASE_URL"] != "" {
		found = append(found, loginServer{api: env["API_BASE_URL"], from: "this gateway's " + deployEnvFile})
	}
	if conf, err := parseDotEnv("/etc/meshploy/node.conf"); err == nil && conf["MESHPLOY_API_URL"] != "" {
		// The mesh address a worker joined through keeps working whatever
		// the public domain does, so it is kept as reached.
		found = append(found, loginServer{api: conf["MESHPLOY_API_URL"], from: "this node's /etc/meshploy/node.conf", keepAPI: true})
	}
	if saved, err := config.Load(); err == nil && saved.APIURL != "" {
		found = append(found, loginServer{api: saved.APIURL, from: "the last login"})
	}
	for _, f := range found {
		srv, err := reachServer(f.api, f.from)
		if err != nil {
			continue
		}
		srv.keepAPI = f.keepAPI
		fmt.Printf("Logging in to %s, from %s. Use --url for another server.\n", displayHost(srv.api), f.from)
		return srv, nil
	}

	fmt.Print("Meshploy domain (for example example.com): ")
	if !sc.Scan() || strings.TrimSpace(sc.Text()) == "" {
		return loginServer{}, errors.New("no server given: run meshploy auth login --url <your domain>")
	}
	return reachServer(sc.Text(), "")
}

// reachServer turns what was typed or found into the API address that
// answers, and the console name it named, if any.
func reachServer(given, from string) (loginServer, error) {
	var tried []string
	for _, c := range serverCandidates(given) {
		if _, err := client.New(c.api, "").LoginInfo(); err == nil {
			return loginServer{api: c.api, door: c.door, from: from}, nil
		}
		tried = append(tried, c.api)
	}
	return loginServer{}, fmt.Errorf("no Meshploy server answered at %s", strings.Join(tried, " or "))
}

type serverCandidate struct{ api, door string }

// serverCandidates are the API addresses something typed may mean, in order:
//
//	example.com                 -> https://api.example.com
//	api.example.com             -> https://api.example.com
//	console.example.com         -> https://api.example.com, approved at console
//	apps.example.com            -> https://api.example.com, approved at apps
//	https://api.example.com/x   -> https://api.example.com
//	http://localhost:4000       -> as given (an address with a port is the API)
//
// A name of three or more labels is tried both as a door on its parent domain
// and as a base domain itself (deploy.example.co.uk).
func serverCandidates(given string) []serverCandidate {
	given = strings.TrimSpace(given)
	if !strings.Contains(given, "://") {
		given = "https://" + given
	}
	u, err := url.Parse(given)
	if err != nil || u.Hostname() == "" {
		return nil
	}
	host := strings.ToLower(u.Hostname())
	if u.Port() != "" || net.ParseIP(host) != nil || !strings.Contains(host, ".") {
		return []serverCandidate{{api: u.Scheme + "://" + u.Host}}
	}
	label, rest, _ := strings.Cut(host, ".")
	if label == "api" {
		return []serverCandidate{{api: u.Scheme + "://" + host}}
	}
	var out []serverCandidate
	if strings.Contains(rest, ".") {
		door := label
		if door == "console" {
			door = ""
		}
		out = append(out, serverCandidate{api: u.Scheme + "://api." + rest, door: door})
	}
	return append(out, serverCandidate{api: u.Scheme + "://api." + host})
}

// displayHost is an API address as a person names the server: its domain.
func displayHost(api string) string {
	u, err := url.Parse(api)
	if err != nil || u.Host == "" {
		return api
	}
	if h := u.Hostname(); strings.HasPrefix(h, "api.") && u.Port() == "" {
		return strings.TrimPrefix(h, "api.")
	}
	return u.Host
}

// waitForApproval polls until the login is approved, denied or expires.
func waitForApproval(api string, login client.CLILogin) (string, error) {
	c := client.New(api, "")
	interval := time.Duration(login.Interval) * time.Second
	if interval <= 0 {
		interval = 5 * time.Second
	}
	for time.Now().Before(login.ExpiresAt) {
		time.Sleep(interval)
		st, err := c.PollCLILogin(login.DeviceCode)
		if errors.Is(err, client.ErrSlowDown) {
			interval += 5 * time.Second
			continue
		}
		if err != nil {
			return "", err
		}
		switch st.Status {
		case "pending":
			continue
		case "approved":
			return st.Token, nil
		case "denied":
			return "", errors.New("the login was denied in the browser")
		case "collected":
			return "", errors.New("this login was already used: run meshploy auth login again")
		default:
			return "", errors.New("the code expired before it was approved: run meshploy auth login again")
		}
	}
	return "", errors.New("the code expired before it was approved: run meshploy auth login again")
}

// openBrowser opens url where this machine has a browser to open it in. Over
// SSH it does not try: a browser there would open on the remote machine.
func openBrowser(link string) bool {
	if os.Getenv("SSH_CONNECTION") != "" || os.Getenv("SSH_TTY") != "" {
		return false
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", link)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", link)
	default:
		if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
			return false
		}
		cmd = exec.Command("xdg-open", link)
	}
	return cmd.Start() == nil
}

// passwordLogin is the login for a machine with no browser to approve from:
// email, password and two-factor code typed here, for the console's
// 24-hour session.
func passwordLogin(cmd *cobra.Command, sc *bufio.Scanner, apiURL string) error {
	email, _ := cmd.Flags().GetString("email")
	if email == "" {
		fmt.Print("Email: ")
		sc.Scan()
		email = strings.TrimSpace(sc.Text())
	}

	password, err := readSecret(sc, "Password")
	if err != nil {
		return err
	}

	c := client.New(apiURL, "")
	result, err := c.Login(email, password)
	if err != nil {
		return fmt.Errorf("login failed: %w", err)
	}

	token := result.Token
	if result.TOTPRequired {
		code, err := readSecret(sc, "Two-factor code")
		if err != nil {
			return err
		}
		token, err = c.CompleteTOTPLogin(result.MFAToken, strings.TrimSpace(code))
		if err != nil {
			return fmt.Errorf("2FA verification failed: %w", err)
		}
	}
	return saveLogin(apiURL, token)
}

// saveLogin keeps the server and the credential, with the organisation this
// install serves.
func saveLogin(apiURL, token string) error {
	apiURL = strings.TrimRight(apiURL, "/")
	orgs, err := client.New(apiURL, token).ListOrgs()
	if err != nil || len(orgs) == 0 {
		return fmt.Errorf("login succeeded but could not resolve org: %w", err)
	}
	if err := config.Save(&config.Config{APIURL: apiURL, Token: token, OrgID: orgs[0].ID}); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	fmt.Printf("✔  Logged in to %s, organisation %s. Config saved to ~/.meshploy/config.json\n", displayHost(apiURL), orgs[0].Slug)
	return nil
}

var logoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Log this CLI out and remove saved credentials",
	RunE: func(cmd *cobra.Command, args []string) error {
		// A browser login's token is revoked on the server too, so it stops
		// working everywhere, not just here.
		if saved, err := config.Load(); err == nil && strings.HasPrefix(saved.Token, "mcli-") {
			if err := client.New(saved.APIURL, saved.Token).LogoutCLI(); err != nil {
				fmt.Fprintf(os.Stderr, "warning: the server did not revoke this login (%v); it lapses after 90 days unused\n", err)
			}
		}
		if err := config.Clear(); err != nil {
			return err
		}
		fmt.Println("✔  Logged out.")
		return nil
	},
}

var whoamiCmd = &cobra.Command{
	Use:   "whoami",
	Short: "Print the saved API URL and authentication status",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil || cfg.APIURL == "" {
			fmt.Println("Not logged in. Run: meshploy auth login")
			return nil
		}
		fmt.Println("API URL:", cfg.APIURL)
		if cfg.Token != "" {
			preview := cfg.Token
			if len(preview) > 12 {
				preview = preview[:12] + "…"
			}
			fmt.Println("Token:  ", preview)
		}
		return nil
	},
}

func init() {
	loginCmd.Flags().String("url", "", "The server: a domain, or a console, Apps or API address")
	loginCmd.Flags().Bool("password", false, "Type email, password and two-factor code here instead of approving in a browser")
	loginCmd.Flags().Bool("no-browser", false, "Print the link without trying to open a browser")
	loginCmd.Flags().StringP("email", "e", "", "Email address, with --password")
	authCmd.AddCommand(loginCmd, logoutCmd, whoamiCmd)
	rootCmd.AddCommand(authCmd)
}

package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/meshploy/apps/cli/internal/setup"
	"github.com/spf13/cobra"
)

var (
	setupServeAddr     string
	setupServePublicIP string
	// setupServeDemo runs the page against stand-ins, for working on it
	// without a server to install.
	setupServeDemo bool
	// setupServeDemoPlatform is what the demo host pretends to run: "dokploy",
	// or "none" for a fresh server.
	setupServeDemoPlatform string
)

// setupServeCmd runs the browser-driven installer.
//
// It lives in the CLI because the binary is already on the box before any
// container is: get.sh installs it, then calls `meshploy node install`. That
// also sidesteps the trap that rules out a container here — a container cannot
// bind the mesh IP during phase 1, because tailscale0 does not exist yet.
var setupServeCmd = &cobra.Command{
	Use:   "serve",
	Short: "Serve the browser-based installer on this machine",
	Long: `Serves a setup page that collects the domain and DNS mode, runs the
installer, and streams its output to the browser.

Requires root, because it writes /opt/meshploy/.env, generates the CoreDNS,
Headscale and Caddy configuration, and drives compose. Access is gated on the
setup token from /opt/meshploy/.env — this listens on a public address over
plain HTTP, because no certificate exists yet.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if setupServeDemo {
			return serveSetupDemo(cmd.Context(), cmd.OutOrStdout(), cmd.Flags().Changed("addr"))
		}
		if os.Getuid() != 0 {
			return fmt.Errorf("setup serve requires root — re-run with sudo")
		}

		token := readEnvVar("SETUP_TOKEN")
		if token == "" {
			// Refuse rather than serve an unauthenticated installer. Anything
			// that can reach this can claim the machine.
			// Not `setup-token rotate`: it refuses when no token exists, which is
			// exactly this case. The installer is what creates one.
			return errors.New("no SETUP_TOKEN in " + meshployInstDir + "/.env; start setup through the installer, which creates one: sudo bash -c \"$(curl -fsSL https://meshploy.com/install.sh)\"")
		}

		publicIP := setupServePublicIP
		if publicIP == "" {
			publicIP = readEnvVar("PUBLIC_IP")
		}

		store, err := setup.NewStore(meshployInstDir)
		if err != nil {
			return err
		}
		// Seed from .env so a re-run edits the current configuration rather than
		// starting blank. This is what makes "change the domain" work: there is
		// no console-side form for it — the API container mounts two read-only
		// files and has no Docker socket, so it cannot rewrite the CoreDNS and
		// Headscale configs the domain is baked into, nor restart them. Serving
		// setup again is the supported way to change it, and install.sh
		// preserves the database and certificates on a re-run.
		_ = store.Update(func(st *setup.State) {
			if st.Answers.PublicIP == "" {
				st.Answers.PublicIP = publicIP
			}
			if st.Answers.Domain == "" {
				st.Answers.Domain = readEnvVar("DOMAIN")
			}
			if st.Answers.DNSMode == "" {
				st.Answers.DNSMode = readEnvVar("DNS_MODE")
			}
			if st.Answers.MeshIP == "" {
				st.Answers.MeshIP = readEnvVar("MESH_IP")
			}
		})

		setupSrv := setup.NewServer(
			store, token,
			setup.PublicResolver(""),
			setup.ScriptRunner{Script: meshployInstDir + "/install.sh", Dir: meshployInstDir},
			setup.DokployPlanner{},
		)
		srv := &http.Server{
			Addr:    setupServeAddr,
			Handler: setupSrv.Handler(),
			// Generous: an install runs for minutes and streams the whole time.
			ReadHeaderTimeout: 15 * time.Second,
		}

		ln, err := net.Listen("tcp", setupServeAddr)
		if err != nil {
			return fmt.Errorf("listen on %s: %w", setupServeAddr, err)
		}

		// install.sh opens 80, 443, 53 and 3478 and nothing else, so on a host
		// with an active firewall — most fresh cloud images — this page would
		// otherwise be unreachable from the browser it exists for. The rule is
		// owned by this process and removed when it stops: a setup port left
		// open afterwards is a privileged installer left exposed.
		closePort := func() {}
		if _, port, perr := net.SplitHostPort(ln.Addr().String()); perr == nil {
			if n, cerr := strconv.Atoi(port); cerr == nil {
				var opened bool
				closePort, opened = setup.NewPortGuard().Open(cmd.Context(), n)
				if opened {
					fmt.Printf("  Opened port %s in the firewall for the duration of setup.\n", port)
				}
			}
		}
		defer closePort()

		fmt.Printf("\n  Setup is available at:\n\n")
		for _, u := range setupURLs(publicIP, ln.Addr()) {
			fmt.Printf("      %s\n", u)
		}
		fmt.Printf("\n  Sign in with the setup token from %s/.env\n", meshployInstDir)
		fmt.Printf("  Press Ctrl+C to stop serving.\n\n")

		// Stop cleanly on Ctrl+C: a half-written .env is worse than no install,
		// and the in-flight installer needs the chance to finish its write.
		ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		go func() {
			// Either the operator interrupts, or they finish and leave for the
			// console. Both stop the installer — leaving it listening once it is
			// no longer needed is the liability.
			select {
			case <-ctx.Done():
			case <-setupSrv.Done():
				fmt.Println("\n  Setup complete — the console is yours. Stopping the installer.")
			}
			shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_ = srv.Shutdown(shutdown)
		}()

		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	},
}

// setupURLs lists the addresses the page is reachable on, so the operator does
// not have to work out what ":9000" means from a machine they may only have a
// console on.
func setupURLs(publicIP string, addr net.Addr) []string {
	_, port, err := net.SplitHostPort(addr.String())
	if err != nil {
		return []string{"http://<this-server>" + setupServeAddr}
	}
	out := []string{}
	if publicIP != "" {
		out = append(out, "http://"+net.JoinHostPort(publicIP, port))
	}
	out = append(out, "http://"+net.JoinHostPort("localhost", port))
	return out
}

// setupCmd groups the browser-driven install. Kept separate from setup-token,
// which is referenced by name in install.sh output and in the API's error when
// a registration is refused — renaming that would break printed instructions.
var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Browser-driven installation of this gateway",
}

func init() {
	setupServeCmd.Flags().StringVar(&setupServeAddr, "addr", "0.0.0.0:9000",
		"Address to serve the setup page on")
	setupServeCmd.Flags().StringVar(&setupServePublicIP, "public-ip", "",
		"Public IP to pre-fill and to check DNS against (defaults to PUBLIC_IP from .env)")
	setupServeCmd.Flags().BoolVar(&setupServeDemo, "demo", false,
		"Serve the page against stand-ins on localhost, installing nothing (for working on the page)")
	setupServeCmd.Flags().StringVar(&setupServeDemoPlatform, "demo-platform", "dokploy",
		"What the demo host pretends to run: dokploy, or none for a fresh server")
	_ = setupServeCmd.Flags().MarkHidden("demo")
	_ = setupServeCmd.Flags().MarkHidden("demo-platform")
	setupCmd.AddCommand(setupServeCmd)
	rootCmd.AddCommand(setupCmd)
}

// serveSetupDemo runs the setup page against stand-ins on the loopback
// address: nothing is installed, no host is read, and its state lives in a
// temporary directory. For working on the page itself.
func serveSetupDemo(ctx context.Context, out io.Writer, ownAddr bool) error {
	dir, err := os.MkdirTemp("", "meshploy-setup-demo-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)

	srv, err := setup.NewDemoServer(dir, setupServeDemoPlatform)
	if err != nil {
		return err
	}

	// Loopback unless asked otherwise: this serves a page that accepts a token
	// printed on the terminal, which is fine on this machine and nowhere else.
	addr := "127.0.0.1:7788"
	if ownAddr {
		addr = setupServeAddr
	}
	fmt.Fprintf(out, "Setup demo on http://%s\n  token: %s\n  host: %s\n  nothing is installed and no host is read.\n",
		addr, setup.DemoToken, setupServeDemoPlatform)

	httpSrv := &http.Server{Addr: addr, Handler: srv.Handler(), ReadHeaderTimeout: 15 * time.Second}
	go func() {
		<-ctx.Done()
		_ = httpSrv.Close()
	}()
	if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

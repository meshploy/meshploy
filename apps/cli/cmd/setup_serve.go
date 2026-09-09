package cmd

import (
	"context"
	"errors"
	"fmt"
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
		if os.Getuid() != 0 {
			return fmt.Errorf("setup serve requires root — re-run with sudo")
		}

		token := readEnvVar("SETUP_TOKEN")
		if token == "" {
			// Refuse rather than serve an unauthenticated installer. Anything
			// that can reach this can claim the machine.
			return errors.New("no SETUP_TOKEN in " + meshployInstDir + "/.env — run install.sh first, or `meshploy setup-token rotate`")
		}

		publicIP := setupServePublicIP
		if publicIP == "" {
			publicIP = readEnvVar("PUBLIC_IP")
		}

		store, err := setup.NewStore(meshployInstDir)
		if err != nil {
			return err
		}
		// Seed the public IP so the operator confirms a detected value rather
		// than typing one, and so the DNS checks have something to compare to.
		if publicIP != "" && store.Get().Answers.PublicIP == "" {
			_ = store.Update(func(st *setup.State) { st.Answers.PublicIP = publicIP })
		}

		setupSrv := setup.NewServer(
			store, token,
			setup.PublicResolver(""),
			setup.ScriptRunner{Script: meshployInstDir + "/install.sh", Dir: meshployInstDir},
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
	setupCmd.AddCommand(setupServeCmd)
	rootCmd.AddCommand(setupCmd)
}

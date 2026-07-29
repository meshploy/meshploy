package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/meshploy/packages/client"
)

// ceImage is the stock API image. MESHPLOY_API_IMAGE overrides the repository
// half of docker-compose.yml's `image:`; unset means this.
const ceImage = "ghcr.io/meshploy/api"

// defaultEEImage is used when a licence carries no explicit registry scope.
const defaultEEImage = "ghcr.io/meshploy/api-ee"

// applyEEImage points MESHPLOY_API_IMAGE at the Enterprise image and makes sure
// the host can pull it.
//
// The image is the only difference between a CE and an EE install: the licence
// is already stored server-side, and every feature gate reads it at runtime. So
// "upgrading" is one env var plus a restart — which is why this lives in
// server-upgrade rather than being a separate workflow.
func applyEEImage(image, pat string) error {
	if image == "" {
		image = defaultEEImage
	}

	current := readEnvVar("MESHPLOY_API_IMAGE")
	if current == image {
		fmt.Printf("✔  Already configured for %s\n", image)
	} else {
		if err := setEnvVar("MESHPLOY_API_IMAGE", image); err != nil {
			return fmt.Errorf("set MESHPLOY_API_IMAGE: %w", err)
		}
		fmt.Printf("✔  Switched the API image to %s\n", image)
	}

	// A private image needs credentials. Check before pulling so the failure
	// names the cause rather than surfacing as a compose error.
	if pullable(image) {
		return nil
	}

	if pat == "" {
		return fmt.Errorf("cannot pull %s — the Enterprise image is a private package.\n"+
			"Authenticate first, then re-run:\n\n"+
			"  echo $GITHUB_PAT | docker login ghcr.io -u <github-username> --password-stdin\n\n"+
			"or pass a token with --token (needs read:packages)", image)
	}

	user := os.Getenv("GHCR_USER")
	if user == "" {
		user = "meshploy"
	}
	fmt.Println("Authenticating to ghcr.io…")
	login := exec.Command("docker", "login", "ghcr.io", "--username", user, "--password-stdin")
	login.Stdin = strings.NewReader(pat)
	login.Stdout, login.Stderr = os.Stdout, os.Stderr
	if err := login.Run(); err != nil {
		return fmt.Errorf("docker login ghcr.io: %w — check the token has read:packages, "+
			"and set GHCR_USER if your username differs", err)
	}
	if !pullable(image) {
		return fmt.Errorf("authenticated, but %s is still not pullable — "+
			"does this licence grant access to it?", image)
	}
	return nil
}

// pullable reports whether the image manifest can be read with the credentials
// currently available to the daemon.
func pullable(image string) bool {
	// Any tag would do; latest is what compose resolves to by default.
	return exec.Command("docker", "manifest", "inspect", image+":latest").Run() == nil
}

// eeNotice tells an operator running a plain upgrade that their licence entitles
// them to more than they are running. It never changes anything: a silent image
// swap during a routine upgrade would be a surprise, and this command already
// restarts the whole stack.
func eeNotice(currentImage, scope string) {
	if scope == "" || currentImage == scope {
		return
	}
	fmt.Printf("\n  This install is licensed for %s but is running %s.\n", scope, currentImage)
	fmt.Printf("  Switch to it with: sudo meshploy server-upgrade --ee\n\n")
}

// currentAPIImage reports the image the stack is configured to run.
func currentAPIImage() string {
	if v := readEnvVar("MESHPLOY_API_IMAGE"); v != "" {
		return v
	}
	return ceImage
}

// entitledRegistryScope asks the API which private image this install's licence
// grants, so the operator never has to know the repository path.
//
// Best-effort by design. This command runs under sudo on the gateway, where the
// saved CLI credentials belong to the invoking user's home directory and may
// not be readable — and an unlicensed install has no scope at all. Neither is
// an error: the caller falls back to defaultEEImage or simply says nothing.
func entitledRegistryScope() string {
	// Deliberately not apiClient(): that exits the process when no credentials
	// are saved, which would turn a routine upgrade on an unconfigured host into
	// a hard failure.
	if loadedCfg == nil || loadedCfg.APIURL == "" {
		return ""
	}
	ent, err := client.New(loadedCfg.APIURL, loadedCfg.Token).GetEntitlements()
	if err != nil || ent == nil || !ent.Licensed {
		return ""
	}
	return ent.RegistryScope
}

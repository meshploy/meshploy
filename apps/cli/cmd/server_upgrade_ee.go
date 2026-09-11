package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/meshploy/packages/client"
	"github.com/meshploy/packages/license"
)

// ceImage and ceWebImage are the stock API and console images.
// MESHPLOY_API_IMAGE and MESHPLOY_WEB_IMAGE override the repository half of
// docker-compose.yml's `image:`; unset means these.
const (
	ceImage    = "ghcr.io/meshploy/api"
	ceWebImage = "ghcr.io/meshploy/web"
)

// productRefLabel is stamped on every Enterprise image by its build: vX.Y.Z for
// an image built from that Community release, main for one built from main.
const productRefLabel = "com.meshploy.product.ref"

// defaultEEImage is used when a licence carries no explicit registry scope.
const defaultEEImage = license.DefaultRegistryScope

// eeImageFromScope returns the image a licence's registry scope points at, or
// the default Enterprise image when the licence names none.
//
// The scope is checked with the rule the issuer applies, so a malformed one
// stops here with a clear message instead of being written to
// MESHPLOY_API_IMAGE and surfacing later as a failed pull.
func eeImageFromScope(scope string) (string, error) {
	if scope == "" {
		return defaultEEImage, nil
	}
	if err := license.ValidRegistryScope(scope); err != nil {
		return "", fmt.Errorf("%w\nPass the image with --ee-image, or ask for a reissued licence", err)
	}
	return scope, nil
}

// pairedWebImage returns the console image that ships with an API image: the
// same repository with the leading "api" of its name replaced by "web", so
// ghcr.io/meshploy/api-ee-acme pairs with ghcr.io/meshploy/web-ee-acme. A
// licence names only the API image; the Enterprise build publishes both under
// these names.
func pairedWebImage(apiImage string) (string, error) {
	dir, name := "", apiImage
	if i := strings.LastIndex(apiImage, "/"); i >= 0 {
		dir, name = apiImage[:i+1], apiImage[i+1:]
	}
	if !strings.HasPrefix(name, "api") {
		return "", fmt.Errorf("cannot tell which console image goes with %s: the Enterprise images are named api-ee… and web-ee…", apiImage)
	}
	return dir + "web" + strings.TrimPrefix(name, "api"), nil
}

// applyEEImage points MESHPLOY_API_IMAGE and MESHPLOY_WEB_IMAGE at the
// Enterprise images and makes sure the host can pull both.
//
// The images are the only difference between a CE and an EE install: the
// licence is already stored server-side, and every feature gate reads it at
// runtime. So "upgrading" is two env vars plus a restart, which is why this
// lives in server-upgrade rather than being a separate workflow.
func applyEEImage(runtime, image, pat string) error {
	if image == "" {
		image = defaultEEImage
	}
	web, err := pairedWebImage(image)
	if err != nil {
		return err
	}

	for _, v := range []struct{ key, what, image string }{
		{"MESHPLOY_API_IMAGE", "API", image},
		{"MESHPLOY_WEB_IMAGE", "console", web},
	} {
		if readEnvVar(v.key) == v.image {
			fmt.Printf("✔  The %s image is already %s\n", v.what, v.image)
			continue
		}
		if err := setEnvVar(v.key, v.image); err != nil {
			return fmt.Errorf("set %s: %w", v.key, err)
		}
		fmt.Printf("✔  Switched the %s image to %s\n", v.what, v.image)
	}

	// Private images need credentials, granted per package, so each is checked.
	// Checking before the pull names the cause rather than surfacing as a
	// compose error.
	images := []string{image, web}
	fmt.Printf("Checking access to %s…\n", strings.Join(images, " and "))
	missing := firstUnpullable(runtime, images)
	if missing == "" {
		return nil
	}

	if pat == "" {
		return fmt.Errorf("cannot pull %s: the Enterprise images are private packages.\n"+
			"Authenticate first, then re-run:\n\n"+
			"  echo $GITHUB_PAT | %s login ghcr.io -u <github-username> --password-stdin\n\n"+
			"or pass a token with --token (needs read:packages)", missing, runtime)
	}

	fmt.Println("Authenticating to ghcr.io…")
	login := exec.Command(runtime, "login", "ghcr.io", "--username", ghcrUser(), "--password-stdin")
	login.Stdin = strings.NewReader(pat)
	login.Stdout, login.Stderr = os.Stdout, os.Stderr
	if err := login.Run(); err != nil {
		return fmt.Errorf("%s login ghcr.io: %w\n"+
			"The token must carry read:packages, must not have expired, and must "+
			"belong to an account granted read on both packages", runtime, err)
	}
	if missing := firstUnpullable(runtime, images); missing != "" {
		return fmt.Errorf("authenticated to ghcr.io, but %s is still not pullable: "+
			"this account has no read grant on that package", missing)
	}
	return nil
}

// firstUnpullable returns the first image the host cannot pull, or "".
func firstUnpullable(runtime string, images []string) string {
	for _, image := range images {
		if !pullable(runtime, image) {
			return image
		}
	}
	return ""
}

// checkEnterpriseImages stops an upgrade when a configured Enterprise image was
// built from another Community version than the one being installed. ref is
// the release tag being installed (vX.Y.Z), or main on edge.
//
// The Enterprise build follows each Community release by minutes. An
// Enterprise server upgrading inside that gap would run the previous
// Enterprise image under the new release's configuration, and a switch could
// put an image built on an older API core against a newer database. The label
// says which Community version is inside, so a mismatch stops here, after the
// pull and before anything restarts.
func checkEnterpriseImages(dir, runtime, ref, channel string) error {
	for _, image := range enterpriseImages() {
		tagged := image + ":" + channel
		out, err := runtimeOutput(dir, runtime, "image", "inspect", "--format",
			`{{ index .Config.Labels "`+productRefLabel+`" }}`, tagged)
		built := strings.TrimSpace(string(out))
		if err != nil || built == "" || built == "<no value>" {
			built = "an unrecorded version"
		}
		if built != ref {
			return fmt.Errorf("the Enterprise build for %s is not published yet: %s was built from %s.\n"+
				"Nothing was changed. Run this again once the Enterprise images for %s are out", ref, tagged, built, ref)
		}
	}
	return nil
}

// enterpriseImages lists the configured images that are not Community ones.
func enterpriseImages() []string {
	var out []string
	if v := readEnvVar("MESHPLOY_API_IMAGE"); v != "" && v != ceImage {
		out = append(out, v)
	}
	if v := readEnvVar("MESHPLOY_WEB_IMAGE"); v != "" && v != ceWebImage {
		out = append(out, v)
	}
	return out
}

// pullable reports whether the image can be fetched with the credentials the
// runtime currently holds.
//
// A pull rather than `manifest inspect`: podman's manifest command operates on
// local manifest lists and does not answer "can I reach this remote image". The
// product installer settled on the same probe for the same reason. Nothing is
// wasted when it succeeds — `compose pull` runs moments later and finds the
// layers already cached.
func pullable(runtime, image string) bool {
	return exec.Command(runtime, "pull", image+":"+pullChannel()).Run() == nil
}

// ghcrUser is the account name to present to ghcr.io.
//
// install.sh records the username it authenticated with, so a re-login here
// uses the same account. Read from .env rather than the environment because
// this command runs under sudo, which resets the environment by default — an
// exported GHCR_USER would never reach us.
//
// The fallback covers an install that declined registry login, leaving nothing
// recorded. ghcr derives identity from the token and does not validate this
// field: our own CI proves it, logging in as github.actor with a token owned by
// github-actions[bot]. x-access-token is GitHub's convention for token auth.
func ghcrUser() string {
	if u := readEnvVar("GHCR_USER"); u != "" {
		return u
	}
	return "x-access-token"
}

// pullChannel is the tag compose will actually resolve, rather than a hardcoded
// latest: an install following the edge channel has no :latest of its own, so
// probing for one would report the image unreachable when it is not.
func pullChannel() string {
	if c := readEnvVar("MESHPLOY_CHANNEL"); c != "" {
		return c
	}
	return "latest"
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

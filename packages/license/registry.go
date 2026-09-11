package license

import (
	"fmt"
	"regexp"
)

// DefaultRegistryScope is the Enterprise API image, for a licence that names no
// other. A vendor build is published beside it, e.g. ghcr.io/meshploy/api-ee-acme.
const DefaultRegistryScope = "ghcr.io/meshploy/api-ee"

// registryScopePattern accepts the Enterprise API image or a vendor build of it
// (api-ee-<name>) under ghcr.io/meshploy/, and nothing else: no other image of
// Meshploy's, no other registry, no nested path, no tag, no digest.
//
// Only api-ee names, because the console image pairs with the API image by
// name (api-ee-acme with web-ee-acme), and because a switch should never be
// able to point the API service at an image built for something else, such as
// the operator console.
var registryScopePattern = regexp.MustCompile(`^ghcr\.io/meshploy/api-ee(?:[._-][a-z0-9]+)*$`)

// ValidRegistryScope returns nil when scope is an image a licence may name, and
// the reason when it is not.
//
// The scope is written by the issuer and acted on by the product:
// `server-upgrade --ee` points the API service at it, and a switch to
// Enterprise from the console has the host pull it as root. Both sides apply
// this one rule, which is why it lives here beside the feature names. A scope
// that fails it would sign and verify cleanly, then fail only when a customer's
// server tried to pull it.
//
// Confining it to Meshploy's Enterprise API builds means a licence, or anything
// able to influence what one says, can at most choose among those, and pulling
// any of them still needs registry credentials the host holds. It names a
// repository only, because the tag follows the server's channel.
func ValidRegistryScope(scope string) error {
	if registryScopePattern.MatchString(scope) {
		return nil
	}
	return fmt.Errorf("registry scope %q is not an Enterprise image: it must be %s or a vendor build of it such as %s-acme, with no tag", scope, DefaultRegistryScope, DefaultRegistryScope)
}

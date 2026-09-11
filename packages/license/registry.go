package license

import (
	"fmt"
	"regexp"
)

// DefaultRegistryScope is the Enterprise API image, for a licence that names no
// other. A vendor build is published beside it, e.g. ghcr.io/meshploy/api-ee-acme.
const DefaultRegistryScope = "ghcr.io/meshploy/api-ee"

// registryScopePattern accepts one repository under ghcr.io/meshploy/ and
// nothing else: no other registry, no nested path, no tag, no digest.
var registryScopePattern = regexp.MustCompile(`^ghcr\.io/meshploy/[a-z0-9]+(?:[._-][a-z0-9]+)*$`)

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
// Confining it to Meshploy's own repository means a licence, or anything able
// to influence what one says, can at most choose among images Meshploy
// publishes, and pulling any of them still needs registry credentials the host
// holds. It names a repository only, because the tag follows the server's
// channel.
func ValidRegistryScope(scope string) error {
	if registryScopePattern.MatchString(scope) {
		return nil
	}
	return fmt.Errorf("registry scope %q is not an Enterprise image: it must be one repository under ghcr.io/meshploy/, with no tag, such as %s", scope, DefaultRegistryScope)
}

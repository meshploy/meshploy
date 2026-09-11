package server

import (
	"net/http"
	"testing"

	"github.com/meshploy/packages/server/version"
)

// The load-bearing case: the Community binary imports no extension, so every
// hook is empty and it reports Community. The moment anything registers, it is
// an Enterprise build, which is how the Enterprise binary identifies itself
// without a build flag to forget.
func TestDetectEditionFollowsTheExtensionHooks(t *testing.T) {
	saved := middlewareHooks
	t.Cleanup(func() { middlewareHooks = saved })
	middlewareHooks = nil

	if got := detectEdition(); got != version.EditionCommunity {
		t.Fatalf("no extension registered: got %q, want community", got)
	}

	RegisterMiddleware(PriorityAfterAuth, func(next http.Handler) http.Handler { return next })
	if got := detectEdition(); got != version.EditionEnterprise {
		t.Fatalf("an extension registered: got %q, want enterprise", got)
	}
}

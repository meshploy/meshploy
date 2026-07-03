package templates

import "embed"

// builtinFS is a pinned snapshot of a few reference templates, compiled into the
// binary. It is the last-resort catalog: served only when no local TEMPLATE_DIR
// is configured AND the remote catalog is unreachable or hasn't loaded yet
// (cold start, offline, air-gapped). A successful live fetch supersedes it.
//
// Keep this in sync with the meshploy-templates repo. It is a resilience floor,
// not the source of truth — it need not contain the entire catalog, just enough
// that a fresh or disconnected install is never empty.
//
//go:embed builtin
var builtinFS embed.FS

// NewEmbeddedCatalog serves the pinned snapshot via the standard Registry over
// the embedded filesystem.
func NewEmbeddedCatalog() Catalog {
	return NewRegistry(builtinFS, "builtin")
}

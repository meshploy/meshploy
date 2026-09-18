package setup

import (
	"embed"
	_ "embed"
	"net/http"
	"strings"
)

// wizardHTML is the setup page.
//
// Embedded, and deliberately dependency-free: no framework, no CDN, no build
// step. The web container cannot serve this — it renders a console that needs
// the API, which needs Postgres, which needs an answer this page has not
// collected yet — and the machine it runs on may have no outbound internet at
// the moment it is needed most.
//
//go:embed assets/wizard.html
var wizardHTML []byte

// fontFS carries the console's typefaces, for the same reason the page carries
// its own CSS: this often runs on a machine whose DNS does not work yet - that
// is frequently why the operator is here - and a webfont request to a CDN would
// hang, rendering the page unstyled at the worst possible moment. Serving them
// from the binary is what lets setup look like the console it installs.
//
//go:embed assets/fonts
var fontFS embed.FS

// fonts serves them. Public: a typeface is not a secret, and gating it on the
// setup token would mean the token screen itself renders in a fallback face.
func fonts() http.Handler {
	h := http.FileServer(http.FS(fontFS))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Immutable: these change only when the binary does.
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		if strings.HasSuffix(r.URL.Path, ".ttf") {
			w.Header().Set("Content-Type", "font/ttf")
		}
		h.ServeHTTP(w, r)
	})
}

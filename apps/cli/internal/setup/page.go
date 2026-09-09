package setup

import _ "embed"

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

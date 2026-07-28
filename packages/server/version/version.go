package version

// Current is the running version — "dev" in local builds,
// overridden at build time with -ldflags "-X ...version.Current=x.y.z".
var Current = "dev"

package version

// Current is the running version — "dev" in local builds, overridden at build
// time with -ldflags "-X ...version.Current=x.y.z".
//
// An edge build carries the commit it was cut from: "0.8.0+a1b2c3d". That build
// is somewhere AHEAD of 0.8.0, not equal to it, and the suffix is what says so.
var Current = "dev"

// Channel is "stable" for a build cut from a release tag, "edge" for one built
// from main, "dev" for a local build.
//
// The update check needs it. An edge build compared against the newest release
// looks current the moment that release lands, so without the channel an
// operator tracking main is told they are up to date while running code the
// release does not contain — and told to upgrade to code they already have.
var Channel = "dev"

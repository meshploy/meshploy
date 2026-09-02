package service

import "testing"

// An edge build carries the commit it was cut from as a "+sha" suffix. That
// suffix is the only thing distinguishing it from the release it names, and the
// only thing an edge update check has to compare against.
func TestBuildCommit(t *testing.T) {
	cases := []struct{ in, want string }{
		{"0.8.0+a1b2c3d", "a1b2c3d"},
		{"0.8.0", ""}, // a release build
		{"dev", ""},   // a local build
		{"", ""},      // nothing recorded
	}
	for _, tc := range cases {
		if got := buildCommit(tc.in); got != tc.want {
			t.Errorf("buildCommit(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// A stable build compares against releases; the edge path must not be reached
// for it, and an edge build with no recorded commit must not guess.
func TestEdgeCheckNeedsACommit(t *testing.T) {
	s := &SystemService{}
	info := s.edgeVersionInfo(t.Context(), VersionInfo{Current: "0.8.0", Channel: channelEdge})
	if info.UpdateAvailable {
		t.Error("claimed an update with no commit to compare — that is a guess, not a check")
	}
}

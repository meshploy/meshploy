package service

import "time"

// Hooks for node removal tests in package service_test.

// SetHeadscaleForTest points node removal at a test Headscale.
func (s *NodeService) SetHeadscaleForTest(h *HeadscaleService) { s.headscale = h }

// NoRemovalWaitsForTest makes Remove try Headscale once, without pausing, and
// returns a function that restores the retries.
func NoRemovalWaitsForTest() func() {
	saved := removalRetries
	removalRetries = []time.Duration{0}
	return func() { removalRetries = saved }
}

package service_test

import (
	"regexp"
	"testing"
	"time"

	"github.com/meshploy/packages/server/service"
	"github.com/stretchr/testify/require"
)

// GitHub App names are unique across GitHub and at most 34 characters: the
// brand alone is taken after the first App, so each gets the day and a suffix.
func TestGitHubAppNamesAreUniqueAndFit(t *testing.T) {
	day := time.Date(2026, 10, 25, 23, 0, 0, 0, time.UTC)

	a, b := service.AppName("Meshploy", day), service.AppName("Meshploy", day)
	require.Regexp(t, regexp.MustCompile(`^Meshploy-2026-10-25-[a-z0-9]{6}$`), a)
	require.NotEqual(t, a, b, "two Apps made the same day need different names")

	long := service.AppName("Acme Platform Engineering Group", day)
	require.LessOrEqual(t, len(long), 34)
	require.Regexp(t, regexp.MustCompile(`^Acme Platform-2026-10-25-[a-z0-9]{6}$`), long, "a long brand is cut at a whole word")

	require.Regexp(t, regexp.MustCompile(`^Meshploy-2026-10-25-`), service.AppName("  ", day), "no brand falls back to Meshploy")
}

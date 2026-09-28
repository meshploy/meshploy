package service_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A migration that takes the edge first sends the domains the old platform
// still serves to its edge on a side port: one fallback, replaced whole,
// cleared at the end, and only its hostnames get certificates through it.
func TestEdgeFallbackServesOnlyItsHostnames(t *testing.T) {
	ctx := context.Background()
	svcs := newServices(newTestDB(t))

	f, err := svcs.EdgeFallback.Get(ctx)
	require.NoError(t, err)
	assert.Nil(t, f)
	assert.False(t, svcs.EdgeFallback.Serves(ctx, "settrip.in"))

	_, err = svcs.EdgeFallback.Set(ctx, "not-an-address", nil)
	assert.Error(t, err)

	_, err = svcs.EdgeFallback.Set(ctx, "127.0.0.1:18080", []string{"Settrip.in", "settrip.in", "minio.settrip.in"})
	require.NoError(t, err)
	f, err = svcs.EdgeFallback.Set(ctx, "127.0.0.1:18081", []string{"settrip.in"})
	require.NoError(t, err)
	assert.Equal(t, "127.0.0.1:18081", f.Upstream)
	assert.True(t, svcs.EdgeFallback.Serves(ctx, "SETTRIP.in"))
	assert.False(t, svcs.EdgeFallback.Serves(ctx, "minio.settrip.in"), "replaced whole")

	require.NoError(t, svcs.EdgeFallback.Clear(ctx))
	assert.False(t, svcs.EdgeFallback.Serves(ctx, "settrip.in"))
}

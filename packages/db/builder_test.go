package db_test

import (
	"testing"

	"github.com/meshploy/packages/db"
	"github.com/stretchr/testify/require"
)

// Nixpacks was retired: a client, a stack file or an older row that still
// names it builds with Railpack, and so does naming nothing.
func TestBuilderMeansRailpackForNixpacksAndNothing(t *testing.T) {
	require.Equal(t, db.BuilderRailpack, db.Builder("nixpacks"))
	require.Equal(t, db.BuilderRailpack, db.Builder(""))
	require.Equal(t, db.BuilderDockerfile, db.Builder(db.BuilderDockerfile))
	require.Equal(t, db.BuilderImage, db.Builder(db.BuilderImage))
}

package dnsprovider_test

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/meshploy/packages/server/dnsprovider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestZoneForPrefersTheLongestMatch(t *testing.T) {
	zones := []dnsprovider.Zone{{ID: "a", Name: "example.com"}, {ID: "b", Name: "eu.example.com"}, {ID: "c", Name: "ample.com"}}
	z, ok := dnsprovider.ZoneFor(zones, "acme.eu.example.com")
	require.True(t, ok)
	assert.Equal(t, "b", z.ID)
	z, ok = dnsprovider.ZoneFor(zones, "example.com")
	require.True(t, ok)
	assert.Equal(t, "a", z.ID, "a zone's own name is in it")
	_, ok = dnsprovider.ZoneFor(zones, "notexample.com")
	assert.False(t, ok, "a suffix that is not a parent label is not a match")
}

func pointingFixture(t *testing.T) (*fakeCloudflare, dnsprovider.Provider, dnsprovider.Zone) {
	t.Helper()
	cf := &fakeCloudflare{token: "good", writable: map[string]bool{"z1": true}, records: map[string][]dnsprovider.Record{}}
	srv := httptest.NewServer(cf)
	t.Cleanup(srv.Close)
	return cf, dnsprovider.NewCloudflare("good", srv.URL), dnsprovider.Zone{ID: "z1", Name: "example.com"}
}

// Pointing a domain writes its wildcard and, with nothing there yet, the name
// itself; pointing again writes nothing more; unpointing takes away exactly
// what was written.
func TestPointAndUnpointADomain(t *testing.T) {
	cf, p, zone := pointingFixture(t)
	ctx := context.Background()

	require.NoError(t, dnsprovider.PointDomain(ctx, p, zone, "acme.example.com", "203.0.113.7", "meshploy:d1"))
	require.Len(t, cf.records["z1"], 2)
	for _, r := range cf.records["z1"] {
		assert.Equal(t, "A", r.Type)
		assert.Equal(t, "203.0.113.7", r.Content)
		assert.Equal(t, "meshploy:d1", r.Comment)
	}
	ok, err := dnsprovider.Pointed(ctx, p, zone, "acme.example.com", "203.0.113.7")
	require.NoError(t, err)
	assert.True(t, ok)

	require.NoError(t, dnsprovider.PointDomain(ctx, p, zone, "acme.example.com", "203.0.113.7", "meshploy:d1"))
	assert.Len(t, cf.records["z1"], 2, "pointing twice writes nothing twice")

	require.NoError(t, dnsprovider.UnpointDomain(ctx, p, zone, "acme.example.com", "meshploy:d1"))
	assert.Empty(t, cf.records["z1"])
}

// The operator's records are theirs: one already pointing the wildcard here
// is used, one pointing it elsewhere stops the add, a name with records of its
// own gets none of Meshploy's, and unpointing leaves all of them.
func TestPointingLeavesTheOperatorsRecordsAlone(t *testing.T) {
	cf, p, zone := pointingFixture(t)
	ctx := context.Background()
	cf.records["z1"] = []dnsprovider.Record{
		{ID: "op1", Type: "A", Name: "*.acme.example.com", Content: "203.0.113.7"},
		{ID: "op2", Type: "MX", Name: "acme.example.com", Content: "mail.example.com"},
	}
	require.NoError(t, dnsprovider.PointDomain(ctx, p, zone, "acme.example.com", "203.0.113.7", "meshploy:d1"))
	assert.Len(t, cf.records["z1"], 2, "nothing written: the wildcard already answers, and the name has records")
	require.NoError(t, dnsprovider.UnpointDomain(ctx, p, zone, "acme.example.com", "meshploy:d1"))
	assert.Len(t, cf.records["z1"], 2, "nothing removed that Meshploy did not write")

	cf.records["z1"] = []dnsprovider.Record{{ID: "op3", Type: "CNAME", Name: "*.other.example.com", Content: "elsewhere.net"}}
	err := dnsprovider.PointDomain(ctx, p, zone, "other.example.com", "203.0.113.7", "meshploy:d2")
	assert.ErrorIs(t, err, dnsprovider.ErrConflict)
	assert.Contains(t, err.Error(), "elsewhere.net")
}

// The gateway's address moved: Meshploy's own record is replaced, not left
// beside the new one.
func TestPointingReplacesItsOwnStaleRecord(t *testing.T) {
	cf, p, zone := pointingFixture(t)
	ctx := context.Background()
	require.NoError(t, dnsprovider.PointDomain(ctx, p, zone, "acme.example.com", "203.0.113.7", "meshploy:d1"))
	require.NoError(t, dnsprovider.PointDomain(ctx, p, zone, "acme.example.com", "198.51.100.9", "meshploy:d1"))
	var wild []string
	for _, r := range cf.records["z1"] {
		if r.Name == "*.acme.example.com" {
			wild = append(wild, r.Content)
		}
	}
	assert.Equal(t, []string{"198.51.100.9"}, wild)
}

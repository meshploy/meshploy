package dnsprovider_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/meshploy/packages/server/dnsprovider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeCloudflare answers like Cloudflare's API for two zones, of which the
// token may write only in writable.
type fakeCloudflare struct {
	mu       sync.Mutex
	token    string
	writable map[string]bool
	records  map[string][]dnsprovider.Record // by zone ID
	next     int
}

func (f *fakeCloudflare) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	reply := func(status int, result any, errs ...map[string]any) {
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": status < 300, "errors": errs, "result": result,
			"result_info": map[string]int{"page": 1, "total_pages": 1},
		})
	}
	if r.Header.Get("Authorization") != "Bearer "+f.token {
		reply(http.StatusForbidden, nil, map[string]any{"code": 9109, "message": "Invalid access token"})
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	switch {
	case r.Method == http.MethodGet && len(parts) == 1 && parts[0] == "zones":
		reply(http.StatusOK, []map[string]string{{"id": "z1", "name": "example.com"}, {"id": "z2", "name": "example.org"}})
	case len(parts) >= 3 && parts[2] == "dns_records":
		zone := parts[1]
		switch r.Method {
		case http.MethodGet:
			out := []dnsprovider.Record{}
			for _, rec := range f.records[zone] {
				if rec.Name == r.URL.Query().Get("name") {
					out = append(out, rec)
				}
			}
			reply(http.StatusOK, out)
		case http.MethodPost:
			if !f.writable[zone] {
				reply(http.StatusForbidden, nil, map[string]any{"code": 10000, "message": "Authentication error"})
				return
			}
			var rec dnsprovider.Record
			_ = json.NewDecoder(r.Body).Decode(&rec)
			f.next++
			rec.ID = "r" + string(rune('0'+f.next))
			f.records[zone] = append(f.records[zone], rec)
			reply(http.StatusOK, rec)
		case http.MethodDelete:
			kept := f.records[zone][:0]
			for _, rec := range f.records[zone] {
				if rec.ID != parts[3] {
					kept = append(kept, rec)
				}
			}
			f.records[zone] = kept
			reply(http.StatusOK, map[string]string{"id": parts[3]})
		}
	default:
		reply(http.StatusNotFound, nil, map[string]any{"code": 7003, "message": "not found"})
	}
}

// A check finds which zones the token may write in, and leaves no record
// behind in any of them.
func TestCheckFindsTheZonesATokenMayWriteIn(t *testing.T) {
	cf := &fakeCloudflare{token: "good", writable: map[string]bool{"z1": true}, records: map[string][]dnsprovider.Record{}}
	srv := httptest.NewServer(cf)
	defer srv.Close()

	zones, err := dnsprovider.Check(context.Background(), dnsprovider.NewCloudflare("good", srv.URL))
	require.NoError(t, err)
	assert.Equal(t, []dnsprovider.Zone{
		{ID: "z1", Name: "example.com", CanEdit: true},
		{ID: "z2", Name: "example.org", CanEdit: false},
	}, zones)
	assert.Empty(t, cf.records["z1"], "the check record is removed again")
}

func TestCheckRefusesATokenThatCannotWriteAnywhere(t *testing.T) {
	cf := &fakeCloudflare{token: "good", writable: map[string]bool{}, records: map[string][]dnsprovider.Record{}}
	srv := httptest.NewServer(cf)
	defer srv.Close()

	_, err := dnsprovider.Check(context.Background(), dnsprovider.NewCloudflare("good", srv.URL))
	assert.ErrorIs(t, err, dnsprovider.ErrNoZones)
}

func TestAWrongTokenIsRejected(t *testing.T) {
	cf := &fakeCloudflare{token: "good", writable: map[string]bool{"z1": true}, records: map[string][]dnsprovider.Record{}}
	srv := httptest.NewServer(cf)
	defer srv.Close()

	_, err := dnsprovider.Check(context.Background(), dnsprovider.NewCloudflare("wrong", srv.URL))
	assert.True(t, errors.Is(err, dnsprovider.ErrRejected), "got %v", err)
	assert.Contains(t, err.Error(), "Invalid access token", "the provider's own reason is kept")
}

// Records are written DNS only, with the comment that marks them as
// Meshploy's, and can be found and removed by name.
func TestRecordsRoundTrip(t *testing.T) {
	cf := &fakeCloudflare{token: "good", writable: map[string]bool{"z1": true}, records: map[string][]dnsprovider.Record{}}
	srv := httptest.NewServer(cf)
	defer srv.Close()
	p := dnsprovider.NewCloudflare("good", srv.URL)
	ctx := context.Background()
	zone := dnsprovider.Zone{ID: "z1", Name: "example.com"}

	rec, err := p.CreateRecord(ctx, zone, dnsprovider.Record{Type: "A", Name: "*.example.com", Content: "203.0.113.7", Comment: "meshploy:x"})
	require.NoError(t, err)
	require.NotEmpty(t, rec.ID)
	got, err := p.Records(ctx, zone, "*.example.com")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "meshploy:x", got[0].Comment)
	require.NoError(t, p.DeleteRecord(ctx, zone, rec.ID))
	got, err = p.Records(ctx, zone, "*.example.com")
	require.NoError(t, err)
	assert.Empty(t, got)
}

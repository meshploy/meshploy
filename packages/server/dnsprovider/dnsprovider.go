// Package dnsprovider writes DNS records through a provider's API, for an
// on-demand base domain whose zone stays with that provider. The zone is still
// the operator's: Meshploy writes only records it can recognise as its own, by
// their comment, and removes only those.
package dnsprovider

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
)

// Kind names a provider. Each kind is one client here and one Caddy module in
// the edge image, which proves control of a name through the same token.
type Kind string

const Cloudflare Kind = "cloudflare"

// Kinds lists every provider a domain can use.
var Kinds = []Kind{Cloudflare}

var (
	// ErrRejected: the provider refused the token itself (wrong, revoked,
	// expired, or not allowed from this address).
	ErrRejected = errors.New("the provider rejected the token")
	// ErrNoZones: the token works but sees no zone it may write records in.
	ErrNoZones = errors.New("the token cannot edit records in any zone")
)

// Zone is a zone the token can see. CanEdit is whether it may also write
// records there, which reading alone does not tell.
type Zone struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	CanEdit bool   `json:"can_edit"`
}

// Record is one DNS record. Name is the full name, without a trailing dot.
type Record struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
	TTL     int    `json:"ttl"`
	Comment string `json:"comment"`
}

// Provider is what Meshploy needs from a DNS host.
type Provider interface {
	// Zones lists the zones the token can see.
	Zones(ctx context.Context) ([]Zone, error)
	// Records lists the records named name in zone.
	Records(ctx context.Context, zone Zone, name string) ([]Record, error)
	// CreateRecord writes r into zone and returns it with its ID. A provider
	// that refuses the write for lack of permission returns ErrRejected.
	CreateRecord(ctx context.Context, zone Zone, r Record) (Record, error)
	// DeleteRecord removes the record with this ID from zone.
	DeleteRecord(ctx context.Context, zone Zone, id string) error
}

// New returns the client for kind, authenticated by token.
func New(kind Kind, token string) (Provider, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, errors.New("a token is required")
	}
	switch kind {
	case Cloudflare:
		return NewCloudflare(token, ""), nil
	}
	return nil, fmt.Errorf("unknown DNS provider %q", kind)
}

// checkName is the record a check writes and removes again: under a label of
// its own, so it can never collide with anything the operator keeps.
const checkName = "_meshploy-check"

// CheckComment marks the record a check writes, so a check cut short leaves
// something recognisable behind.
const CheckComment = "meshploy:check"

// Check lists the zones the token sees and finds out which it may write in, by
// writing a TXT record under _meshploy-check in each and removing it again:
// providers do not say what a token may do, only what it did. It fails with
// ErrNoZones when it may write in none.
func Check(ctx context.Context, p Provider) ([]Zone, error) {
	zones, err := p.Zones(ctx)
	if err != nil {
		return nil, err
	}
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		errs []error
		sem  = make(chan struct{}, 4)
	)
	for i := range zones {
		wg.Add(1)
		go func(z *Zone) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			ok, err := canEdit(ctx, p, *z)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", z.Name, err))
				return
			}
			z.CanEdit = ok
		}(&zones[i])
	}
	wg.Wait()
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	for _, z := range zones {
		if z.CanEdit {
			return zones, nil
		}
	}
	return zones, ErrNoZones
}

func canEdit(ctx context.Context, p Provider, z Zone) (bool, error) {
	nonce := make([]byte, 8)
	_, _ = rand.Read(nonce)
	r, err := p.CreateRecord(ctx, z, Record{
		Type:    "TXT",
		Name:    checkName + "." + z.Name,
		Content: "meshploy token check " + hex.EncodeToString(nonce),
		TTL:     60,
		Comment: CheckComment,
	})
	if errors.Is(err, ErrRejected) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, p.DeleteRecord(ctx, z, r.ID)
}

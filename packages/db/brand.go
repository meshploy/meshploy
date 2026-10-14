package db

import (
	"context"
	"net"
	"strings"
	"sync"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Brand is how an organisation names and shows itself in what people read:
// the console, the emails it sends and the edge's own pages. Commands, paths
// and tools always say Meshploy, whatever the brand.
type Brand struct {
	Name string `json:"name"`
	// Accent is a #rrggbb colour for the console's primary tokens, with the
	// text colour drawn on it; both empty keep the console's own.
	Accent           string `json:"accent,omitempty"`
	AccentForeground string `json:"accent_foreground,omitempty"`
	// LogoURL is the wordmark and MarkURL the square mark, both served from
	// the console's own origin; empty keeps Meshploy's.
	LogoURL string `json:"logo_url,omitempty"`
	MarkURL string `json:"mark_url,omitempty"`
	// Custom is false for Meshploy's own brand.
	Custom bool `json:"custom"`
	// PoweredBy shows "Powered by Meshploy" beside a custom brand.
	PoweredBy bool `json:"powered_by"`
}

// DefaultBrand is Meshploy's own, which every organisation has unless an
// edition gives it another.
var DefaultBrand = Brand{Name: "Meshploy"}

// BrandSource gives an organisation's brand, or false to keep the default.
type BrandSource func(ctx context.Context, gdb *gorm.DB, orgID uuid.UUID) (Brand, bool)

var (
	brandMu     sync.RWMutex
	brandSource BrandSource
)

// RegisterBrandSource sets where brands come from. Call from an extension's
// init(), in the API and in the proxy alike. Community registers none, so
// every organisation shows Meshploy's.
func RegisterBrandSource(src BrandSource) {
	brandMu.Lock()
	defer brandMu.Unlock()
	brandSource = src
}

func currentBrandSource() BrandSource {
	brandMu.RLock()
	defer brandMu.RUnlock()
	return brandSource
}

// BrandOf is an organisation's brand.
func BrandOf(ctx context.Context, gdb *gorm.DB, orgID uuid.UUID) Brand {
	src := currentBrandSource()
	if src == nil || orgID == uuid.Nil {
		return DefaultBrand
	}
	if b, ok := src(ctx, gdb, orgID); ok && strings.TrimSpace(b.Name) != "" {
		b.Custom = true
		return b
	}
	return DefaultBrand
}

// BrandForHost is the brand of the organisation whose verified domain host
// sits under, for a page shown before anyone signs in. A host under no domain
// (an address, localhost) shows the default.
func BrandForHost(ctx context.Context, gdb *gorm.DB, host string) Brand {
	if currentBrandSource() == nil {
		return DefaultBrand
	}
	d, err := BaseDomainFor(ctx, gdb, host)
	if err != nil || d == nil {
		return DefaultBrand
	}
	return BrandOf(ctx, gdb, d.OrganizationID)
}

// BaseDomainFor returns the verified base domain a hostname sits under, or nil.
// A port on the host is ignored.
//
// Matching is done in Go over the domains rather than in SQL: there are a
// handful of them, and the alternative is a suffix comparison the database
// cannot index anyway.
func BaseDomainFor(ctx context.Context, gdb *gorm.DB, hostname string) (*Domain, error) {
	host := strings.ToLower(strings.TrimSpace(hostname))
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.TrimSuffix(host, ".")
	if host == "" {
		return nil, nil
	}
	domains := make([]Domain, 0)
	if err := gdb.WithContext(ctx).Where("verified = ?", true).Find(&domains).Error; err != nil {
		return nil, err
	}
	var best *Domain
	for i := range domains {
		if !strings.HasSuffix(host, "."+domains[i].BaseDomain) {
			continue
		}
		// Longest wins, so a domain delegated under another one is not shadowed
		// by its parent.
		if best == nil || len(domains[i].BaseDomain) > len(best.BaseDomain) {
			best = &domains[i]
		}
	}
	return best, nil
}

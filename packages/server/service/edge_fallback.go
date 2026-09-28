package service

import (
	"context"
	"errors"
	"fmt"
	"net"
	"slices"
	"strings"

	"github.com/meshploy/packages/db"
	"gorm.io/gorm"
)

// EdgeFallbackService keeps the one fallback upstream a migration sets when it
// takes the edge before moving everything: see db.EdgeFallback.
type EdgeFallbackService struct {
	db *gorm.DB
}

// Get is the fallback in place, or nil when there is none.
func (s *EdgeFallbackService) Get(ctx context.Context) (*db.EdgeFallback, error) {
	var f db.EdgeFallback
	err := s.db.WithContext(ctx).Order("created_at").First(&f).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &f, nil
}

// Set replaces the fallback.
func (s *EdgeFallbackService) Set(ctx context.Context, upstream string, hostnames []string) (*db.EdgeFallback, error) {
	host, port, err := net.SplitHostPort(strings.TrimSpace(upstream))
	if err != nil || net.ParseIP(host) == nil || port == "" {
		return nil, fmt.Errorf("the upstream is an address and a port, such as 127.0.0.1:18080, not %q", upstream)
	}
	hosts := db.StringArray{}
	for _, h := range hostnames {
		h = strings.ToLower(strings.TrimSpace(h))
		if h != "" && !slices.Contains(hosts, h) {
			hosts = append(hosts, h)
		}
	}
	f := db.EdgeFallback{Upstream: net.JoinHostPort(host, port), Hostnames: hosts}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("1 = 1").Delete(&db.EdgeFallback{}).Error; err != nil {
			return err
		}
		return tx.Create(&f).Error
	})
	if err != nil {
		return nil, err
	}
	return &f, nil
}

// Clear removes the fallback: the old edge is no longer reached.
func (s *EdgeFallbackService) Clear(ctx context.Context) error {
	return s.db.WithContext(ctx).Where("1 = 1").Delete(&db.EdgeFallback{}).Error
}

// Serves reports whether the fallback serves a hostname, which is what lets
// Caddy get a certificate for it.
func (s *EdgeFallbackService) Serves(ctx context.Context, hostname string) bool {
	f, err := s.Get(ctx)
	return err == nil && f != nil && slices.Contains(f.Hostnames, strings.ToLower(hostname))
}

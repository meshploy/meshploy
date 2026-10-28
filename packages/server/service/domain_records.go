package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/dnsprovider"
	"gorm.io/gorm"
)

// A base domain's records are kept either by hand ("manual") or by a connected
// DNS provider. With a provider, Meshploy writes the records that send the
// domain to the gateway, and the provider's acceptance of them is the proof of
// control: no TXT record to add, and the domain is verified as soon as they
// read back.

// recordComment marks the records Meshploy writes for a domain. It names the
// domain row, so the records of one installation's domain are never mistaken
// for another's, nor for the operator's.
func recordComment(d *db.Domain) string { return "meshploy:" + d.ID.String() }

// CreateThroughProvider adds an on-demand base domain whose records the
// provider writes. It is verified once they read back; if they cannot be
// written, the domain is not added.
func (s *DomainService) CreateThroughProvider(ctx context.Context, orgID uuid.UUID, baseDomain string, providerID uuid.UUID) (*db.Domain, error) {
	domain, err := s.Create(ctx, orgID, baseDomain, db.DNSModeOnDemand)
	if err != nil {
		return nil, err
	}
	out, err := s.SetDNSProvider(ctx, domain.ID, &providerID)
	if err != nil {
		if derr := s.db.WithContext(ctx).Delete(&db.Domain{}, "id = ?", domain.ID).Error; derr != nil {
			log.Printf("warning: could not remove %s after its records failed: %v", domain.BaseDomain, derr)
		}
		return nil, err
	}
	return out, nil
}

// SetDNSProvider chooses who keeps a domain's records: a connected provider,
// or nil for the operator by hand.
//
// To a provider, it writes the records and reads them back, and the domain is
// verified. To manual, it removes the records Meshploy wrote; the domain stays
// verified and served, and its records are the operator's to keep from then.
// Run again with the same provider, it puts back whatever was taken away.
func (s *DomainService) SetDNSProvider(ctx context.Context, domainID uuid.UUID, providerID *uuid.UUID) (*db.Domain, error) {
	domain, err := s.Get(ctx, domainID)
	if err != nil {
		return nil, err
	}
	if providerID == nil {
		if domain.DNSProviderID == nil {
			return domain, nil
		}
		if err := s.unpoint(ctx, domain); err != nil {
			return nil, err
		}
		domain.DNSProviderID = nil
		return domain, s.db.WithContext(ctx).Model(domain).Update("dns_provider_id", nil).Error
	}

	if domain.DNSMode != db.DNSModeOnDemand {
		return nil, huma.Error422UnprocessableEntity(
			"this domain is delegated to the gateway, which answers for it already. A DNS provider is for an on-demand domain")
	}
	if domain.DNSProviderID != nil && *domain.DNSProviderID != *providerID {
		return nil, huma.Error422UnprocessableEntity(
			"another DNS provider writes this domain's records. Switch them to manual first, so that one's are removed")
	}
	if s.publicIP == "" {
		return nil, huma.Error422UnprocessableEntity(
			"this gateway's public address is not known (PUBLIC_IP), so there is nothing to point the records at")
	}
	if s.dns == nil {
		return nil, errors.New("DNS providers are not available")
	}
	provider, err := s.dns.Get(ctx, domain.OrganizationID, *providerID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, huma.Error422UnprocessableEntity("no such DNS provider in this organization")
	}
	if err != nil {
		return nil, err
	}
	client, zone, err := s.dns.zoneFor(ctx, provider, domain.BaseDomain)
	if err != nil {
		return nil, recordsError(provider, "find the zone for", err)
	}
	if err := dnsprovider.PointDomain(ctx, client, zone, domain.BaseDomain, s.publicIP, recordComment(domain)); err != nil {
		return nil, recordsError(provider, "write", err)
	}
	ok, err := dnsprovider.Pointed(ctx, client, zone, domain.BaseDomain, s.publicIP)
	if err != nil {
		return nil, recordsError(provider, "read back", err)
	}
	if !ok {
		return nil, huma.Error422UnprocessableEntity(fmt.Sprintf(
			"%s accepted the records for *.%s but does not return them; try again in a moment", provider.Name, domain.BaseDomain))
	}
	domain.DNSProviderID, domain.Verified = providerID, true
	if err := s.db.WithContext(ctx).Model(domain).
		Updates(map[string]any{"dns_provider_id": *providerID, "verified": true}).Error; err != nil {
		return nil, err
	}
	return domain, nil
}

// unpoint removes the records Meshploy wrote for a domain, if a provider keeps
// them.
//
// A token the provider no longer accepts cannot remove anything, and refusing
// on that would leave the domain impossible to remove or switch. So that one
// failure goes ahead, with the records left named in the log for the operator
// to remove by hand. Any other failure, which trying again can fix, stops.
func (s *DomainService) unpoint(ctx context.Context, domain *db.Domain) error {
	if domain.DNSProviderID == nil || s.dns == nil {
		return nil
	}
	provider, err := s.dns.Get(ctx, domain.OrganizationID, *domain.DNSProviderID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	client, zone, err := s.dns.zoneFor(ctx, provider, domain.BaseDomain)
	if err == nil {
		err = dnsprovider.UnpointDomain(ctx, client, zone, domain.BaseDomain, recordComment(domain))
	}
	if errors.Is(err, dnsprovider.ErrRejected) || errors.Is(err, ErrDNSProviderZone) {
		log.Printf("warning: %s no longer lets Meshploy remove the records it wrote for %s (*.%s and %s, comment %s): %v",
			provider.Name, domain.BaseDomain, domain.BaseDomain, domain.BaseDomain, recordComment(domain), err)
		return nil
	}
	if err != nil {
		return recordsError(provider, "remove", err)
	}
	return nil
}

// recordsError says what a provider refused, in its own words where it gave
// any.
func recordsError(p *db.DNSProvider, what string, err error) error {
	if errors.Is(err, dnsprovider.ErrConflict) {
		return huma.Error409Conflict(err.Error())
	}
	if errors.Is(err, ErrDNSProviderZone) {
		return huma.Error422UnprocessableEntity(err.Error())
	}
	if errors.Is(err, dnsprovider.ErrRejected) {
		return huma.Error422UnprocessableEntity(fmt.Sprintf(
			"%s rejected the token when Meshploy tried to %s the records: %v. Check the provider again from Integrations", p.Name, what, err))
	}
	if he, ok := err.(huma.StatusError); ok {
		return he
	}
	return huma.Error502BadGateway(fmt.Sprintf("could not %s the records through %s: %v", what, p.Name, err))
}

// ErrDNSProviderZone: no zone the token may write in holds the domain.
var ErrDNSProviderZone = errors.New("the DNS provider holds no zone for this domain")

// zoneFor opens the provider's client and finds the zone that holds name among
// those its token may write in.
func (s *DNSProviderService) zoneFor(ctx context.Context, p *db.DNSProvider, name string) (dnsprovider.Provider, dnsprovider.Zone, error) {
	open := s.open
	if open == nil {
		open = dnsprovider.New
	}
	client, err := open(dnsprovider.Kind(p.Kind), string(p.Token))
	if err != nil {
		return nil, dnsprovider.Zone{}, err
	}
	editable := map[string]bool{}
	for _, z := range p.Zones {
		editable[z] = true
	}
	zones, err := client.Zones(ctx)
	if err != nil {
		return nil, dnsprovider.Zone{}, err
	}
	var candidates []dnsprovider.Zone
	for _, z := range zones {
		if editable[z.Name] {
			candidates = append(candidates, z)
		}
	}
	zone, ok := dnsprovider.ZoneFor(candidates, name)
	if !ok {
		held := "none"
		if len(p.Zones) > 0 {
			held = strings.Join(p.Zones, ", ")
		}
		return nil, dnsprovider.Zone{}, fmt.Errorf(
			"%w: %s cannot write records for %s; the zones its token may write in are %s. Check the provider again if that changed",
			ErrDNSProviderZone, p.Name, name, held)
	}
	return client, zone, nil
}

package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/dnsprovider"
	"gorm.io/gorm"
)

var (
	// ErrDNSProviderCheck: the token could not be used as a provider here. The
	// message says why, in the provider's words where it gave any.
	ErrDNSProviderCheck = errors.New("the DNS provider's token did not pass its check")
	// ErrDNSProviderInUse: a domain still has its records written through it.
	ErrDNSProviderInUse = errors.New("the DNS provider is in use")
)

// DNSProviderService keeps the organization's connected DNS providers. A token
// is checked before it is stored, and the zones it may write in are kept with
// it, so the console can offer them without asking the provider each time.
type DNSProviderService struct {
	db *gorm.DB
	// open builds the client for a kind and token; tests replace it.
	open func(kind dnsprovider.Kind, token string) (dnsprovider.Provider, error)
}

// DNSProviderCheck is a provider with what its last check found.
type DNSProviderCheck struct {
	Provider *db.DNSProvider    `json:"provider"`
	Zones    []dnsprovider.Zone `json:"zones"`
}

type CreateDNSProviderInput struct {
	Name  string
	Kind  dnsprovider.Kind
	Token string
}

func (s *DNSProviderService) List(ctx context.Context, orgID uuid.UUID) ([]db.DNSProvider, error) {
	items := make([]db.DNSProvider, 0)
	err := s.db.WithContext(ctx).Where("organization_id = ?", orgID).Order("created_at").Find(&items).Error
	return items, err
}

func (s *DNSProviderService) Get(ctx context.Context, orgID, id uuid.UUID) (*db.DNSProvider, error) {
	var p db.DNSProvider
	if err := s.db.WithContext(ctx).Where("id = ? AND organization_id = ?", id, orgID).First(&p).Error; err != nil {
		return nil, err
	}
	return &p, nil
}

// Create checks the token and stores it only if it may write records in at
// least one zone.
func (s *DNSProviderService) Create(ctx context.Context, orgID uuid.UUID, in CreateDNSProviderInput) (*DNSProviderCheck, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, fmt.Errorf("%w: a name is required", ErrDNSProviderCheck)
	}
	token := strings.TrimSpace(in.Token)
	zones, err := s.check(ctx, in.Kind, token)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	p := &db.DNSProvider{
		OrganizationID: orgID,
		Name:           name,
		Kind:           string(in.Kind),
		Token:          db.EncryptedString(token),
		Zones:          editable(zones),
		CheckedAt:      &now,
	}
	if err := s.db.WithContext(ctx).Create(p).Error; err != nil {
		return nil, err
	}
	return &DNSProviderCheck{Provider: p, Zones: zones}, nil
}

// Check asks the provider again which zones the token may write in, and keeps
// the answer. A token that no longer passes is reported, not removed: the
// domains using it keep what they were given until someone decides.
func (s *DNSProviderService) Check(ctx context.Context, orgID, id uuid.UUID) (*DNSProviderCheck, error) {
	p, err := s.Get(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	zones, err := s.check(ctx, dnsprovider.Kind(p.Kind), string(p.Token))
	if err != nil {
		return nil, err
	}
	now := time.Now()
	p.Zones, p.CheckedAt = editable(zones), &now
	if err := s.db.WithContext(ctx).Model(p).Select("zones", "checked_at").Updates(p).Error; err != nil {
		return nil, err
	}
	return &DNSProviderCheck{Provider: p, Zones: zones}, nil
}

// Delete removes a provider no domain uses.
func (s *DNSProviderService) Delete(ctx context.Context, orgID, id uuid.UUID) error {
	if _, err := s.Get(ctx, orgID, id); err != nil {
		return err
	}
	var using []string
	if err := s.db.WithContext(ctx).Model(&db.Domain{}).
		Where("dns_provider_id = ?", id).Order("base_domain").Pluck("base_domain", &using).Error; err != nil {
		return err
	}
	if len(using) > 0 {
		return fmt.Errorf("%w: %s still has its records written through it; switch it to manual first", ErrDNSProviderInUse, strings.Join(using, ", "))
	}
	return s.db.WithContext(ctx).Where("id = ? AND organization_id = ?", id, orgID).Delete(&db.DNSProvider{}).Error
}

func (s *DNSProviderService) check(ctx context.Context, kind dnsprovider.Kind, token string) ([]dnsprovider.Zone, error) {
	open := s.open
	if open == nil {
		open = dnsprovider.New
	}
	p, err := open(kind, token)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDNSProviderCheck, err)
	}
	zones, err := dnsprovider.Check(ctx, p)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDNSProviderCheck, err)
	}
	return zones, nil
}

func editable(zones []dnsprovider.Zone) []string {
	names := make([]string, 0, len(zones))
	for _, z := range zones {
		if z.CanEdit {
			names = append(names, z.Name)
		}
	}
	return names
}

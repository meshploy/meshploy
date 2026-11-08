package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
	"gorm.io/gorm"
)

type RegistryService struct {
	db *gorm.DB
}

type CreateRegistryInput struct {
	Name      string
	Provider  db.RegistryProvider
	Endpoint  string
	Namespace string
	Username  string
	Password  string
}

func (s *RegistryService) List(ctx context.Context, orgID uuid.UUID) ([]db.RegistryIntegration, error) {
	items := make([]db.RegistryIntegration, 0)
	err := s.db.WithContext(ctx).Where("organization_id = ?", orgID).Find(&items).Error
	return items, err
}

func (s *RegistryService) Create(ctx context.Context, orgID uuid.UUID, in CreateRegistryInput) (*db.RegistryIntegration, error) {
	item := &db.RegistryIntegration{
		OrganizationID: orgID,
		Name:           in.Name,
		Provider:       in.Provider,
		Endpoint:       in.Endpoint,
		Namespace:      in.Namespace,
		Username:       db.EncryptedString(in.Username),
		Password:       db.EncryptedString(in.Password),
	}
	return item, s.db.WithContext(ctx).Create(item).Error
}

func (s *RegistryService) Delete(ctx context.Context, id, orgID uuid.UUID) error {
	// Prevent deleting the built-in registry - it is managed by the platform.
	var reg db.RegistryIntegration
	if err := s.db.WithContext(ctx).Where("id = ? AND organization_id = ?", id, orgID).First(&reg).Error; err != nil {
		return err
	}
	if reg.Provider == db.RegistryBuiltin {
		return fmt.Errorf("built-in registry cannot be removed")
	}
	return s.db.WithContext(ctx).
		Where("id = ? AND organization_id = ?", id, orgID).
		Delete(&db.RegistryIntegration{}).Error
}

// SyncEdgeBuiltin points every organisation's built-in registry at the edge's,
// on an API that reaches its cluster through a cluster agent: the registry runs
// on the edge, at its mesh address, and builds push there and nodes pull from
// there. Made when missing, and moved when the edge's address changed.
func (s *RegistryService) SyncEdgeBuiltin(ctx context.Context) error {
	var edge []db.Node
	if err := s.db.WithContext(ctx).Where("cluster_agent_cert <> ''").Limit(1).Find(&edge).Error; err != nil {
		return err
	}
	if len(edge) == 0 || edge[0].TailscaleIP == "" {
		return nil
	}
	endpoint := edge[0].TailscaleIP + ":5000"
	var orgs []db.Organization
	if err := s.db.WithContext(ctx).Find(&orgs).Error; err != nil {
		return err
	}
	for _, org := range orgs {
		if err := s.SeedBuiltin(ctx, org.ID, endpoint); err != nil {
			return err
		}
		if err := s.db.WithContext(ctx).Model(&db.RegistryIntegration{}).
			Where("organization_id = ? AND provider = ? AND endpoint <> ?", org.ID, db.RegistryBuiltin, endpoint).
			Updates(map[string]any{"endpoint": endpoint, "namespace": endpoint}).Error; err != nil {
			return err
		}
	}
	return nil
}

// SeedBuiltin ensures a built-in registry row exists for the given org.
// It is idempotent - calling it multiple times is safe.
func (s *RegistryService) SeedBuiltin(ctx context.Context, orgID uuid.UUID, endpoint string) error {
	if endpoint == "" {
		return nil
	}
	var count int64
	s.db.WithContext(ctx).Model(&db.RegistryIntegration{}).
		Where("organization_id = ? AND provider = ?", orgID, db.RegistryBuiltin).
		Count(&count)
	if count > 0 {
		return nil
	}
	reg := &db.RegistryIntegration{
		OrganizationID: orgID,
		Name:           "Built-in Registry",
		Provider:       db.RegistryBuiltin,
		Endpoint:       endpoint,
		Namespace:      endpoint,
		Username:       db.EncryptedString(""),
		Password:       db.EncryptedString(""),
	}
	return s.db.WithContext(ctx).Create(reg).Error
}

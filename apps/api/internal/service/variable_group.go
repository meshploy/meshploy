package service

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type VariableGroupService struct {
	db *gorm.DB
}

// ─── Read helpers ─────────────────────────────────────────────────────────────

func (s *VariableGroupService) List(ctx context.Context, projectID uuid.UUID) ([]db.VariableGroup, error) {
	var groups []db.VariableGroup
	err := s.db.WithContext(ctx).
		Preload("Items").
		Where("project_id = ?", projectID).
		Order("system_managed ASC, created_at ASC").
		Find(&groups).Error
	return groups, err
}

func (s *VariableGroupService) Get(ctx context.Context, groupID uuid.UUID) (*db.VariableGroup, error) {
	var g db.VariableGroup
	err := s.db.WithContext(ctx).Preload("Items").First(&g, "id = ?", groupID).Error
	return &g, err
}

// ListForService returns all groups attached to a service (items included).
func (s *VariableGroupService) ListForService(ctx context.Context, serviceID uuid.UUID) ([]db.VariableGroup, error) {
	var svgs []db.ServiceVariableGroup
	if err := s.db.WithContext(ctx).Where("service_id = ?", serviceID).Find(&svgs).Error; err != nil {
		return nil, err
	}
	if len(svgs) == 0 {
		return nil, nil
	}
	ids := make([]uuid.UUID, len(svgs))
	for i, sv := range svgs {
		ids[i] = sv.GroupID
	}
	var groups []db.VariableGroup
	err := s.db.WithContext(ctx).Preload("Items").Where("id IN ?", ids).Find(&groups).Error
	return groups, err
}

// ─── User-managed group CRUD ──────────────────────────────────────────────────

type CreateGroupInput struct {
	ProjectID   uuid.UUID
	Name        string
	Description string
}

func (s *VariableGroupService) Create(ctx context.Context, in CreateGroupInput) (*db.VariableGroup, error) {
	g := db.VariableGroup{
		ProjectID:   in.ProjectID,
		Name:        in.Name,
		Description: in.Description,
	}
	if err := s.db.WithContext(ctx).Create(&g).Error; err != nil {
		return nil, err
	}
	return &g, nil
}

type UpdateGroupInput struct {
	Name        *string
	Description *string
}

func (s *VariableGroupService) Update(ctx context.Context, groupID uuid.UUID, in UpdateGroupInput) (*db.VariableGroup, error) {
	updates := map[string]any{}
	if in.Name != nil {
		updates["name"] = *in.Name
	}
	if in.Description != nil {
		updates["description"] = *in.Description
	}
	if len(updates) > 0 {
		if err := s.db.WithContext(ctx).Model(&db.VariableGroup{}).
			Where("id = ? AND system_managed = false", groupID).
			Updates(updates).Error; err != nil {
			return nil, err
		}
	}
	return s.Get(ctx, groupID)
}

func (s *VariableGroupService) Delete(ctx context.Context, groupID uuid.UUID) error {
	return s.db.WithContext(ctx).
		Where("id = ? AND system_managed = false", groupID).
		Delete(&db.VariableGroup{}).Error
}

// ─── Item CRUD ────────────────────────────────────────────────────────────────

type UpsertItemInput struct {
	Key      string
	Value    string
	IsSecret bool
}

func (s *VariableGroupService) UpsertItem(ctx context.Context, groupID uuid.UUID, in UpsertItemInput) (*db.VariableGroupItem, error) {
	item := db.VariableGroupItem{
		GroupID:  groupID,
		Key:      in.Key,
		Value:    db.EncryptedString(in.Value),
		IsSecret: in.IsSecret,
	}
	err := s.db.WithContext(ctx).
		Where(db.VariableGroupItem{GroupID: groupID, Key: in.Key}).
		Assign(db.VariableGroupItem{Value: db.EncryptedString(in.Value), IsSecret: in.IsSecret}).
		FirstOrCreate(&item).Error
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (s *VariableGroupService) DeleteItem(ctx context.Context, itemID uuid.UUID) error {
	return s.db.WithContext(ctx).Delete(&db.VariableGroupItem{}, "id = ?", itemID).Error
}

// ─── Service attachment ───────────────────────────────────────────────────────

func (s *VariableGroupService) Attach(ctx context.Context, serviceID, groupID uuid.UUID) error {
	svg := db.ServiceVariableGroup{ServiceID: serviceID, GroupID: groupID}
	return s.db.WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		Create(&svg).Error
}

func (s *VariableGroupService) Detach(ctx context.Context, serviceID, groupID uuid.UUID) error {
	// Block detaching a system-managed group from the service that owns it.
	var g db.VariableGroup
	if err := s.db.WithContext(ctx).Select("system_managed, service_id").First(&g, "id = ?", groupID).Error; err != nil {
		return err
	}
	if g.SystemManaged && g.ServiceID != nil && *g.ServiceID == serviceID {
		return fmt.Errorf("cannot detach a service's own system-managed variable group")
	}
	return s.db.WithContext(ctx).
		Where("service_id = ? AND group_id = ?", serviceID, groupID).
		Delete(&db.ServiceVariableGroup{}).Error
}

// ─── System-managed group helpers ─────────────────────────────────────────────

var nonAlphanumRe = regexp.MustCompile(`[^A-Z0-9]+`)

// serviceEnvPrefix converts a service name to an env var prefix.
// "auth-api" → "AUTH_API", "my.service" → "MY_SERVICE"
func serviceEnvPrefix(name string) string {
	upper := strings.ToUpper(name)
	return nonAlphanumRe.ReplaceAllString(upper, "_")
}

// UpsertSystemGroup creates or fully replaces the system-managed variable group
// for a service. Called at service creation and after each successful deploy.
func (s *VariableGroupService) UpsertSystemGroup(ctx context.Context, svc *db.Service, namespace string) error {
	prefix := serviceEnvPrefix(svc.Name)
	host := fmt.Sprintf("%s.%s.svc.cluster.local", svc.Name, namespace)

	// Find or create the group
	var group db.VariableGroup
	err := s.db.WithContext(ctx).
		Where("service_id = ?", svc.ID).
		FirstOrCreate(&group, db.VariableGroup{
			ProjectID:     svc.ProjectID,
			ServiceID:     &svc.ID,
			Name:          svc.Name + " (service)",
			Description:   "Auto-generated service discovery variables",
			SystemManaged: true,
		}).Error
	if err != nil {
		return err
	}

	// Build all items for this service's ports
	items := buildServiceItems(prefix, host, svc.Ports)

	// Replace all items atomically and ensure the group is attached to its own service.
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("group_id = ?", group.ID).Delete(&db.VariableGroupItem{}).Error; err != nil {
			return err
		}
		for _, item := range items {
			item.GroupID = group.ID
			if err := tx.Create(&item).Error; err != nil {
				return err
			}
		}
		// Auto-attach the system group to its own service so vars are injected at deploy time.
		svg := db.ServiceVariableGroup{ServiceID: svc.ID, GroupID: group.ID}
		return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&svg).Error
	})
}

func buildServiceItems(prefix, host string, ports []db.ServicePort) []db.VariableGroupItem {
	var items []db.VariableGroupItem

	add := func(key, value string) {
		items = append(items, db.VariableGroupItem{
			Key:      key,
			Value:    db.EncryptedString(value),
			IsSecret: false,
		})
	}

	for _, p := range ports {
		addr := fmt.Sprintf("%s:%d", host, p.Port)
		if p.IsPrimary {
			// Primary port — no name suffix
			add(prefix+"_HOST", host)
			add(prefix+"_PORT", fmt.Sprintf("%d", p.Port))
			if p.IsHTTP {
				add(prefix+"_URL", "http://"+addr)
			} else {
				add(prefix+"_ADDR", addr)
			}
		} else {
			// Named non-primary port — suffix with uppercased port name
			portPrefix := prefix + "_" + nonAlphanumRe.ReplaceAllString(strings.ToUpper(p.Name), "_")
			add(portPrefix+"_PORT", fmt.Sprintf("%d", p.Port))
			if p.IsHTTP {
				add(portPrefix+"_URL", "http://"+addr)
			} else {
				add(portPrefix+"_ADDR", addr)
			}
		}
	}
	return items
}

// CollectEnvVars loads all variable groups attached to a service and returns
// them as a flat key→value map, ready for injection into the K8s Deployment.
// Items from later-attached groups win on key conflict.
func (s *VariableGroupService) CollectEnvVars(ctx context.Context, serviceID uuid.UUID) (map[string]string, error) {
	groups, err := s.ListForService(ctx, serviceID)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string)
	for _, g := range groups {
		for _, item := range g.Items {
			out[item.Key] = string(item.Value)
		}
	}
	return out, nil
}

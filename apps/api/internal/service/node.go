package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type NodeService struct {
	db *gorm.DB
}

func (s *NodeService) List(ctx context.Context, orgID uuid.UUID) ([]db.Node, error) {
	var nodes []db.Node
	err := s.db.WithContext(ctx).Where("organization_id = ?", orgID).Find(&nodes).Error
	return nodes, err
}

func (s *NodeService) Get(ctx context.Context, nodeID uuid.UUID) (*db.Node, error) {
	var node db.Node
	err := s.db.WithContext(ctx).First(&node, "id = ?", nodeID).Error
	return &node, err
}

func (s *NodeService) Register(ctx context.Context, orgID uuid.UUID, name, tailscaleIP string) (*db.Node, error) {
	node := db.Node{
		OrganizationID: orgID,
		Name:           name,
		TailscaleIP:    tailscaleIP,
		Status:         db.NodeOffline,
	}
	
	err := s.db.WithContext(ctx).Create(&node).Error
	return &node, err
}

func (s *NodeService) Update(ctx context.Context, nodeID uuid.UUID, name string) (*db.Node, error) {
	var node db.Node
	if err := s.db.WithContext(ctx).First(&node, "id = ?", nodeID).Error; err != nil {
		return nil, err
	}
	err := s.db.WithContext(ctx).Model(&node).Update("name", name).Error
	return &node, err
}

func (s *NodeService) Delete(ctx context.Context, nodeID uuid.UUID) error {
	return s.db.WithContext(ctx).Delete(&db.Node{}, "id = ?", nodeID).Error
}

// GenerateRegistrationToken creates or replaces the org's node registration
// token and returns the new token string. Format: mreg-<32 random hex bytes>.
func (s *NodeService) GenerateRegistrationToken(ctx context.Context, orgID uuid.UUID) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	token := "mreg-" + hex.EncodeToString(raw)

	row := db.NodeRegistrationToken{
		OrganizationID: orgID,
		Token:          token,
	}
	// Upsert: replace token if one already exists for this org.
	err := s.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "organization_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"token", "updated_at"}),
		}).
		Create(&row).Error
	if err != nil {
		return "", err
	}
	return token, nil
}

// GetRegistrationToken returns the current registration token for the org,
// or an empty string if none has been generated yet.
func (s *NodeService) GetRegistrationToken(ctx context.Context, orgID uuid.UUID) (string, error) {
	var row db.NodeRegistrationToken
	err := s.db.WithContext(ctx).
		Where("organization_id = ?", orgID).
		First(&row).Error
	if err == gorm.ErrRecordNotFound {
		return "", nil
	}
	return row.Token, err
}

// RegisterWithToken validates a node registration token and creates the node.
// Returns the new node, or an error if the token is invalid.
func (s *NodeService) RegisterWithToken(ctx context.Context, token, name, tailscaleIP string) (*db.Node, error) {
	var row db.NodeRegistrationToken
	if err := s.db.WithContext(ctx).Where("token = ?", token).First(&row).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("invalid registration token")
		}
		return nil, err
	}
	return s.Register(ctx, row.OrganizationID, name, tailscaleIP)
}

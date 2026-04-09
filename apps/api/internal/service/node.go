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

// Register creates a new node. Pass db.K3sRoleServer as the optional role to
// mark the gateway node; defaults to db.K3sRoleAgent if omitted.
func (s *NodeService) Register(ctx context.Context, orgID uuid.UUID, name, tailscaleIP string, role ...db.K3sRole) (*db.Node, error) {
	k3sRole := db.K3sRoleAgent
	if len(role) > 0 && role[0] != "" {
		k3sRole = role[0]
	}
	// Server nodes are the running gateway — seed them as online.
	// Agent nodes start offline until their first heartbeat.
	status := db.NodeOffline
	if k3sRole == db.K3sRoleServer {
		status = db.NodeOnline
	}
	node := db.Node{
		OrganizationID: orgID,
		Name:           name,
		TailscaleIP:    tailscaleIP,
		Status:         status,
		K3sRole:        k3sRole,
	}
	err := s.db.WithContext(ctx).Create(&node).Error
	return &node, err
}

type UpdateNodeInput struct {
	Name     string      // empty = no change
	K3sRole  db.K3sRole  // empty = no change
	MeshRole db.MeshRole // empty = no change
}

func (s *NodeService) Update(ctx context.Context, nodeID uuid.UUID, in UpdateNodeInput) (*db.Node, error) {
	var node db.Node
	if err := s.db.WithContext(ctx).First(&node, "id = ?", nodeID).Error; err != nil {
		return nil, err
	}
	updates := map[string]any{}
	if in.Name != "" {
		updates["name"] = in.Name
	}
	if in.K3sRole != "" {
		updates["k3s_role"] = in.K3sRole
	}
	if in.MeshRole != "" {
		updates["mesh_role"] = in.MeshRole
	}
	if len(updates) > 0 {
		if err := s.db.WithContext(ctx).Model(&node).Updates(updates).Error; err != nil {
			return nil, err
		}
	}
	return &node, nil
}

// UpdateRole sets the k3s role on a node. Used internally during gateway seeding.
func (s *NodeService) UpdateRole(ctx context.Context, nodeID uuid.UUID, role db.K3sRole) error {
	return s.db.WithContext(ctx).Model(&db.Node{}).Where("id = ?", nodeID).Update("k3s_role", role).Error
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
// An optional MeshRole sets the scheduling role (defaults to MeshRoleWorkloadBuilder).
func (s *NodeService) RegisterWithToken(ctx context.Context, token, name, tailscaleIP string, meshRole ...db.MeshRole) (*db.Node, error) {
	var row db.NodeRegistrationToken
	if err := s.db.WithContext(ctx).Where("token = ?", token).First(&row).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("invalid registration token")
		}
		return nil, err
	}
	node, err := s.Register(ctx, row.OrganizationID, name, tailscaleIP)
	if err != nil {
		return nil, err
	}
	role := db.MeshRoleWorkloadBuilder
	if len(meshRole) > 0 && meshRole[0] != "" {
		role = meshRole[0]
	}
	if err := s.db.WithContext(ctx).Model(node).Update("mesh_role", role).Error; err != nil {
		return nil, err
	}
	node.MeshRole = role
	return node, nil
}

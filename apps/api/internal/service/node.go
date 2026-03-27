package service

import (
	"context"

	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
	"gorm.io/gorm"
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

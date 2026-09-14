package service

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
	"gorm.io/gorm"
)

// schedulable refuses pinning to a node nothing can start on: a mesh-only node
// is not in the cluster, so a service, database, job or volume pinned there
// would wait forever. An unknown node passes, to be reported where it was
// before. The error is a 400 the handlers return as it is.
func schedulable(ctx context.Context, gdb *gorm.DB, nodeID *uuid.UUID) error {
	if nodeID == nil {
		return nil
	}
	var node db.Node
	if err := gdb.WithContext(ctx).Select("name", "mesh_role").First(&node, "id = ?", *nodeID).Error; err != nil {
		return nil
	}
	if node.MeshRole == db.MeshRoleMesh {
		return huma.Error400BadRequest(node.Name + " is a mesh-only node: it is not in the cluster, so nothing can run on it. Pin to a cluster node, or leave the node to auto-schedule.")
	}
	return nil
}

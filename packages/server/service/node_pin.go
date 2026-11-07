package service

import (
	"context"
	"errors"
	"slices"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
	"gorm.io/gorm"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// errNodeNotInOrg is a node named for a pin or a target that is not one of the
// organization's: unknown, or another organization's machine.
var errNodeNotInOrg = huma.Error400BadRequest("no such node in this organization")

// nodeInOrg is the organization's node nodeID, or errNodeNotInOrg.
func nodeInOrg(ctx context.Context, gdb *gorm.DB, orgID, nodeID uuid.UUID) (*db.Node, error) {
	var node db.Node
	err := gdb.WithContext(ctx).First(&node, "id = ? AND organization_id = ?", nodeID, orgID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errNodeNotInOrg
	}
	if err != nil {
		return nil, err
	}
	return &node, nil
}

// schedulable refuses pinning projectID's service, database, job or volume to
// a node it cannot run on: one outside the project's organization, or a
// mesh-only node, which is not in the cluster, so what is pinned there would
// wait forever. The errors are 400s the handlers return as they are.
func schedulable(ctx context.Context, gdb *gorm.DB, projectID uuid.UUID, nodeID *uuid.UUID) error {
	if nodeID == nil {
		return nil
	}
	var proj db.Project
	if err := gdb.WithContext(ctx).Select("organization_id").First(&proj, "id = ?", projectID).Error; err != nil {
		return err
	}
	node, err := nodeInOrg(ctx, gdb, proj.OrganizationID, *nodeID)
	if err != nil {
		return err
	}
	if node.MeshRole == db.MeshRoleMesh {
		return huma.Error400BadRequest(node.Name + " is a mesh-only node: it is not in the cluster, so nothing can run on it. Pin to a cluster node, or leave the node to auto-schedule.")
	}
	return nil
}

// builderPinnable refuses pinning projectID's builds to a cluster node that is
// not one of the project's organization's machines. The pin is a Kubernetes
// node name; it is the organization's when one of its nodes has that name or
// the mesh address the cluster reports for it, as the node list matches them.
func builderPinnable(ctx context.Context, gdb *gorm.DB, client kubernetes.Interface, projectID uuid.UUID, k8sName string) error {
	if k8sName == "" {
		return nil
	}
	var proj db.Project
	if err := gdb.WithContext(ctx).Select("organization_id").First(&proj, "id = ?", projectID).Error; err != nil {
		return err
	}
	var nodes []db.Node
	if err := gdb.WithContext(ctx).Select("name", "tailscale_ip").
		Where("organization_id = ?", proj.OrganizationID).Find(&nodes).Error; err != nil {
		return err
	}
	var ips []string
	if client != nil {
		if kn, err := client.CoreV1().Nodes().Get(ctx, k8sName, metav1.GetOptions{}); err == nil {
			for _, a := range kn.Status.Addresses {
				if a.Type == corev1.NodeInternalIP {
					ips = append(ips, a.Address)
				}
			}
		}
	}
	for _, n := range nodes {
		if n.Name == k8sName || (n.TailscaleIP != "" && slices.Contains(ips, n.TailscaleIP)) {
			return nil
		}
	}
	return errNodeNotInOrg
}

package service

import (
	"context"
	"fmt"
	"log"
	"slices"
	"strings"
	"time"

	"github.com/meshploy/packages/db"
	appk8s "github.com/meshploy/packages/server/k8s"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// buildUnschedulableGrace is how long a build pod may go unplaced because no
// node matches it before the deploy fails. Long enough to turn on "Act as build
// node" or bring a builder back online, and far short of the hour a build may
// take.
const buildUnschedulableGrace = 3 * time.Minute

const buildNodeHelp = `Turn on "Act as build node" for a node on the Nodes page, or pick one under Builder node in this service's Config.`

// buildNodeProblem explains why no node can take a build, or returns "" when
// one can. It exists so a build with nowhere to run fails at once, with
// directions, instead of sitting Pending until the build timeout.
//
// A pinned node only has to exist. Otherwise at least one online node must
// carry the build label. Build roles the database holds but the cluster does
// not show yet are applied first: the gateway is a build node by default, and
// its label was otherwise applied only when someone opened the Nodes page.
//
// When the cluster cannot be read, it reports no problem and leaves the
// decision to the scheduler; the wait in runPipeline still fails a build that
// nothing can place.
func (s *DeploymentService) buildNodeProblem(ctx context.Context, pinned string) string {
	if pinned != "" {
		if _, err := s.k8s.CoreV1().Nodes().Get(ctx, pinned, metav1.GetOptions{}); k8serrors.IsNotFound(err) {
			return fmt.Sprintf("The builder node %q set for this service is not in the cluster. Pick another under Builder node in this service's Config.", pinned)
		}
		return ""
	}

	nodes, err := appk8s.GetClusterNodes(ctx, s.k8s)
	if err != nil || readyBuildNodes(nodes) > 0 {
		return ""
	}
	if s.applyBuildRoles(ctx, nodes) {
		if nodes, err = appk8s.GetClusterNodes(ctx, s.k8s); err != nil || readyBuildNodes(nodes) > 0 {
			return ""
		}
	}
	return "No online node can run builds. " + buildNodeHelp
}

func readyBuildNodes(nodes []appk8s.ClusterNode) int {
	n := 0
	for _, cn := range nodes {
		if cn.Ready && appk8s.IsBuildNode(cn.Labels) {
			n++
		}
	}
	return n
}

// applyBuildRoles labels every cluster node whose record has a build role but
// whose labels do not show it yet. It reports whether it labelled any.
func (s *DeploymentService) applyBuildRoles(ctx context.Context, nodes []appk8s.ClusterNode) bool {
	if s.db == nil {
		return false
	}
	var records []db.Node
	if err := s.db.WithContext(ctx).
		Where("mesh_role IN ?", []string{string(db.MeshRoleWorkloadBuilder), string(db.MeshRoleBuilder)}).
		Find(&records).Error; err != nil {
		return false
	}
	changed := false
	for name, role := range buildRoleTargets(nodes, records) {
		if err := appk8s.SetNodeMeshRole(ctx, s.k8s, name, role); err != nil {
			log.Printf("warning: apply build role to node %s: %v", name, err)
			continue
		}
		changed = true
	}
	return changed
}

// buildRoleTargets pairs cluster nodes that lack the build label with the
// build role their record holds, matching a record to a node by name or by
// mesh IP.
func buildRoleTargets(nodes []appk8s.ClusterNode, records []db.Node) map[string]db.MeshRole {
	out := map[string]db.MeshRole{}
	for _, cn := range nodes {
		if appk8s.IsBuildNode(cn.Labels) {
			continue
		}
		for _, r := range records {
			byName := r.Name != "" && r.Name == cn.Name
			byIP := r.TailscaleIP != "" && slices.Contains(cn.InternalIPs, r.TailscaleIP)
			if byName || byIP {
				out[cn.Name] = r.MeshRole
				break
			}
		}
	}
	return out
}

// noBuildNodeMessage explains a build the scheduler could not place.
func noBuildNodeMessage(reason string) string {
	return "No node could run this build: " + strings.TrimSpace(reason) + " " + buildNodeHelp
}

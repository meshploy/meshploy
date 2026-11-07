package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/config"
	"github.com/meshploy/packages/server/middleware"
	"github.com/meshploy/packages/server/service"
)

// A node is reached only through its own organization. An owner of two
// organizations (Community runs one per server; an edition may run more)
// naming one's node under the other gets 404 from every node endpoint, and
// nothing about the node changes.
func TestANodeIsReachedOnlyThroughItsOrganization(t *testing.T) {
	ctx := context.Background()
	database := newAuthzTestDB(t)
	svc := service.New(database, &config.Config{})
	h := New(nil, svc)
	owner, err := svc.Auth.Register(ctx, service.RegisterInput{Username: "owner", Email: "owner@example.com", Password: "password123"})
	if err != nil {
		t.Fatal(err)
	}
	orgs, _ := svc.Orgs.ListForUser(ctx, owner.ID)
	home := orgs[0]
	other := db.Organization{Name: "Other", Slug: "other"}
	if err := database.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Create(&db.OrganizationMember{OrganizationID: other.ID, UserID: owner.ID, Role: db.RoleOwner}).Error; err != nil {
		t.Fatal(err)
	}
	node := db.Node{OrganizationID: home.ID, Name: "worker-1", TailscaleIP: "100.64.0.9", K3sRole: db.K3sRoleAgent, MeshRole: db.MeshRoleWorkload, Status: "online"}
	if err := database.Create(&node).Error; err != nil {
		t.Fatal(err)
	}
	actx := middleware.ContextWithUser(ctx, owner.ID)
	cross := NodePathInput{OrgID: other.ID.String(), NodeID: node.ID.String()}

	want404 := func(what string, err error) {
		t.Helper()
		if statusOf(err) != http.StatusNotFound {
			t.Errorf("%s through another organization: %v, want 404", what, err)
		}
	}
	_, err = h.GetNode(actx, &cross)
	want404("get", err)
	upd := &UpdateNodeInput{OrgID: cross.OrgID, NodeID: cross.NodeID}
	upd.Body.Name = "renamed"
	upd.Body.MeshRole = string(db.MeshRoleBuilder)
	_, err = h.UpdateNode(actx, upd)
	want404("update", err)
	own := &SetNodeOwnerInput{OrgID: cross.OrgID, NodeID: cross.NodeID}
	_, err = h.SetNodeOwner(actx, own)
	want404("set owner", err)
	_, err = h.DeleteNode(actx, &cross)
	want404("delete", err)
	_, err = h.CancelNodeRemoval(actx, &cross)
	want404("cancel removal", err)
	_, err = h.GetNodeMetrics(actx, &cross)
	want404("metrics", err)
	_, err = h.ListNodeContainers(actx, &cross)
	want404("containers", err)
	_, err = h.FreeNodeSpace(actx, &cross)
	want404("free space", err)
	_, err = h.MarkNodeMoved(actx, &cross)
	want404("mark moved", err)
	_, err = h.RevokeClusterAgent(actx, &cross)
	want404("revoke cluster agent", err)

	var after db.Node
	if err := database.First(&after, "id = ?", node.ID).Error; err != nil {
		t.Fatalf("the node is gone: %v", err)
	}
	if after.Name != node.Name || after.MeshRole != node.MeshRole || after.RemovalRequestedAt != nil {
		t.Fatalf("a cross-organization call changed the node: %+v", after)
	}

	// Through its own organization the same owner reaches it.
	got, err := h.GetNode(actx, &NodePathInput{OrgID: home.ID.String(), NodeID: node.ID.String()})
	if err != nil || got.Body.ID != node.ID {
		t.Fatalf("get through its own organization: %v", err)
	}
}

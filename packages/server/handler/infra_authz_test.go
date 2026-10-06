package handler

import (
	"errors"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
)

// What runs on the machines is an owner's or admin's to see: Discovery, its
// ignore marks, and a node's host containers. So is refreshing the server's
// template catalog. A project member is refused each.
func TestInfrastructureDetailIsAnAdminsToSee(t *testing.T) {
	h, orgID, adminCtx, memberCtx := setupClusterAuthz(t)
	forbidden := func(err error) bool {
		var se huma.StatusError
		return errors.As(err, &se) && se.GetStatus() == http.StatusForbidden
	}
	node := uuid.NewString()

	if _, err := h.GetDiscovery(memberCtx, &DiscoveryInput{OrgID: orgID}); !forbidden(err) {
		t.Errorf("a member read Discovery: %v", err)
	}
	ignore := &IgnoreEndpointInput{OrgID: orgID}
	ignore.Body.NodeID = node
	if _, err := h.IgnoreEndpoint(memberCtx, ignore); !forbidden(err) {
		t.Errorf("a member marked an endpoint ignored: %v", err)
	}
	if _, err := h.UnignoreEndpoint(memberCtx, &UnignoreEndpointInput{OrgID: orgID, IgnoreID: uuid.NewString()}); !forbidden(err) {
		t.Errorf("a member cleared an ignore mark: %v", err)
	}
	if _, err := h.ListNodeContainers(memberCtx, &NodePathInput{OrgID: orgID, NodeID: node}); !forbidden(err) {
		t.Errorf("a member read a node's host containers: %v", err)
	}
	if _, err := h.RefreshTemplates(memberCtx, &RefreshTemplatesInput{}); !forbidden(err) {
		t.Errorf("a member refreshed the template catalog: %v", err)
	}

	// An admin gets past the check, whatever the answer is then.
	if _, err := h.GetDiscovery(adminCtx, &DiscoveryInput{OrgID: orgID}); forbidden(err) {
		t.Errorf("an admin was refused Discovery: %v", err)
	}
	if _, err := h.ListNodeContainers(adminCtx, &NodePathInput{OrgID: orgID, NodeID: node}); forbidden(err) {
		t.Errorf("an admin was refused a node's host containers: %v", err)
	}
}

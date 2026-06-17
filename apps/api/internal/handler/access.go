package handler

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
)

// checkAccess authenticates the caller, parses all path IDs, and enforces
// resource-level permission for project-scoped resources (service, stack, job,
// route, volume). Pass parentIDStr="" when there is no parent project in the
// URL (e.g. project-level checks where resourceID IS the project).
//
// Returns the parsed IDs so callers do not need to re-parse them.
func (h *Handler) checkAccess(
	ctx context.Context,
	orgIDStr, resourceIDStr string,
	rt db.ResourceType,
	action db.ResourceAction,
	parentIDStr string,
) (userID, orgID, resourceID uuid.UUID, parentID *uuid.UUID, err error) {
	userID, err = requireUser(ctx)
	if err != nil {
		return
	}
	orgID, err = parseUUID(orgIDStr)
	if err != nil {
		return
	}
	resourceID, err = parseUUID(resourceIDStr)
	if err != nil {
		return
	}
	// projectCheckID is what CheckAccess uses for the org-ownership verification
	// and the project-level grant fallback. For project-type resources the
	// resource IS the project, so we pass it as both resource and parent.
	var projectCheckID *uuid.UUID
	if parentIDStr != "" {
		pid, e := parseUUID(parentIDStr)
		if e != nil {
			err = e
			return
		}
		parentID = &pid
		projectCheckID = parentID
	} else if rt == db.ResourceProject {
		projectCheckID = &resourceID
	}
	if accessErr := h.svc.Permissions.CheckAccess(ctx, orgID, userID, resourceID, rt, action, projectCheckID); accessErr != nil {
		err = huma.Error403Forbidden("access denied")
	}
	return
}

// checkOrgAdminAccess authenticates the caller and enforces admin/owner role
// for org-level admin resources (registry, storage, email config, system backup).
// Pass resourceIDStr="" when there is no specific resource ID in the path.
func (h *Handler) checkOrgAdminAccess(
	ctx context.Context,
	orgIDStr, resourceIDStr string,
) (callerID, orgID, resourceID uuid.UUID, err error) {
	callerID, err = requireUser(ctx)
	if err != nil {
		return
	}
	orgID, err = parseUUID(orgIDStr)
	if err != nil {
		return
	}
	if resourceIDStr != "" {
		resourceID, err = parseUUID(resourceIDStr)
		if err != nil {
			return
		}
	}
	err = h.enforceAdminRole(ctx, orgID, callerID)
	return
}

// checkOrgMemberAccess authenticates the caller and verifies org membership
// for org-level member-accessible resources (git integrations, notifications,
// nodes). Pass resourceIDStr="" when there is no specific resource ID in the
// path.
func (h *Handler) checkOrgMemberAccess(
	ctx context.Context,
	orgIDStr, resourceIDStr string,
) (callerID, orgID, resourceID uuid.UUID, err error) {
	callerID, err = requireUser(ctx)
	if err != nil {
		return
	}
	orgID, err = parseUUID(orgIDStr)
	if err != nil {
		return
	}
	if resourceIDStr != "" {
		resourceID, err = parseUUID(resourceIDStr)
		if err != nil {
			return
		}
	}
	if _, roleErr := h.svc.Orgs.MemberRole(ctx, orgID, callerID); roleErr != nil {
		err = huma.Error403Forbidden("not a member of this organization")
	}
	return
}
